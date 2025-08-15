package submitter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	espresso_client "github.com/EspressoSystems/espresso-network/sdks/go/client"
	tagged_base64 "github.com/EspressoSystems/espresso-network/sdks/go/tagged-base64"
	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/hf/nitrite"
	"github.com/hf/nsm"
	"github.com/hf/nsm/request"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	espresso_key_manager "github.com/offchainlabs/nitro/espresso/key-manager"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

// signedInteger represents any type that is a signed integer, or whose
// underlying type is a signed integer.
type signedInteger interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

// convertToUint64WithFallback is a helper function that converts a signed
// integer value to a type whose underlying type is an unsigned 64-bit integer,
// and returns a fallback value if the signed value is negative. If the value
func convertToUint64WithFallback[T signedInteger, U ~uint64](value T, fallback U) U {
	if value < 0 {
		return fallback
	}

	return U(value)
}

// unsignedInteger represents any type that is an unsigned integer, or whose
// underlying type is an unsigned integer.
type unsignedInteger interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// convertToInt64WithFallback is a helper function that converts an unsigned
// integer value to a type whose underlying type is a signed 64-bit integer,
// and returns a fallback value if the unsigned value is greater than the
// maximum value for a signed 64-bit integer.
func convertToInt64WithFallback[T unsignedInteger, U ~int64](value T, fallback U) U {
	if uint64(value) > 0x7FFF_FFFF_FFFF_FFFF {
		return fallback
	}

	return U(value)
}

// getNumCPUs is a helper function that returns the number of CPUs available
// to process.  This returns the value of [runtime.NumCPU] if it is greater
// than 0, otherwise it returns 1.
//
// NOTE: this function is provided to prevent the linting error for converting
// between an int and a uint.
func getNumCPUs() uint64 {
	// We should always have at least one CPU core available, otherwise
	// how is this code even being run?
	return convertToUint64WithFallback[int, uint64](2, 1)
}

// MultiWorkerQueueEspressoSubmitter is an implementation of `EspressoSubmitter`
// that utilities multiple worker queues to perform Transaction Submission to
// Espresso, and ensure its inclusion.
//
// It approaches the problem by dividing up the task into two separate phases,
// each of which can be scaled independently as needed.  The two phases are
// 1. Submit Transaction Phase: This phase is responsible ensuring that a
// transaction is submitted to Espresso successfully.
// 2. Transaction Inclusion Phase: This phase is responsible for checking if
// a transaction is included in Espresso.
//
// The approach focuses on two main ideas, split up work into two separate
// discrete working units, and allow each unit to scale independently, and
// operate in parallel.
//
// By splitting the work into distinct phases, despite them being related,
// we can ensure that each worker only needs to focus on a single task without
// distracting itself with the other phase, and complicating the logic.
//
// If a Transaction has been successfully submitted to Espresso, but has not
// been included in Espresso within a specified deadline, it will be demoted
// and moved back into the Submission phase, where it will be submitted again
// as if it never was submitted before.
type MultiWorkerQueueEspressoSubmitter struct {
	client espresso_client.EspressoClient

	chainID                       uint64
	resubmissionDeadline          time.Duration
	numSubmitTransactionWorkers   uint64
	numTransactionIncludedWorkers uint64

	submitTxnsQueue    chan SubmitTransactionJob
	submitTxnsJobQueue chan chan SubmitTransactionJob
	submitTxnsResponse chan SubmitTransactionResponse

	transactionIncludedQueue    chan TransactionIncludedJob
	transactionIncludedJobQueue chan chan TransactionIncludedJob
	transactionIncludedResponse chan TransactionIncludedResponse
}

// ErrorFailedToCreateMultiWorkerQueueEspressoSubmitter is an error that is
// returned when the multi-worker queue espresso submitter fails to be created.
type ErrorFailedToCreateMultiWorkerQueueEspressoSubmitter struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToCreateMultiWorkerQueueEspressoSubmitter) Error() string {
	return fmt.Sprintf("failed to create multi-worker queue espresso submitter: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToCreateMultiWorkerQueueEspressoSubmitter) Unwrap() error {
	return e.Cause
}

// NewMultiWorkerQueueEspressoSubmitter creates a new WorkerQueues instance
// with the provided options.
func NewMultiWorkerQueueEspressoSubmitter(options ...EspressoSubmitterConfigOption) (EspressoSubmitter, error) {
	config := DefaultEspressoSubmitterConfig
	applyEspressoSubmitterConfigOptions(&config, options...)
	if err := ValidateEspressoSubmitterConfig(config); err != nil {
		return nil, ErrorFailedToCreateMultiWorkerQueueEspressoSubmitter{Cause: err}
	}

	return &NitroMessageToEspressoTransactionAdapter{
		db:                         config.Db,
		chainID:                    config.ChainID,
		availableTransaction:       make(chan arbutil.MessageIndex, config.MessageIndexQueueSize),
		messageGetter:              config.MessageGetter,
		sendingInterval:            config.EspressoTxnSendingInterval,
		keyManager:                 config.KeyManager,
		userDataAttestationFile:    config.UserDataAttestationFile,
		quoteFile:                  config.QuoteFile,
		espressoMaxTransactionSize: config.EspressoMaxTransactionSize,
		submitter: &MultiWorkerQueueEspressoSubmitter{
			chainID:                       config.ChainID,
			resubmissionDeadline:          config.ResubmitEspressoTxDeadline,
			numSubmitTransactionWorkers:   config.NumberOfSubmitTransactionWorkers,
			numTransactionIncludedWorkers: config.NumberOfTransactionIncludedWorkers,
			client:                        config.EspressoClient,
			submitTxnsQueue:               make(chan SubmitTransactionJob, config.SubmitTransactionsQueueSize),
			transactionIncludedQueue:      make(chan TransactionIncludedJob, config.TransactionIncludedQueueSize),
		},
	}, nil
}

// NitroMessageToEspressoTransactionAdapter is an implementation of
// EspressoSubmitter that adapts Nitro messages to Espresso transactions.
type NitroMessageToEspressoTransactionAdapter struct {
	db                         ethdb.Database
	chainID                    uint64
	submitter                  *MultiWorkerQueueEspressoSubmitter
	availableTransaction       chan arbutil.MessageIndex
	messageGetter              MessageGetter
	espressoMaxTransactionSize int64
	sendingInterval            time.Duration
	keyManager                 espresso_key_manager.EspressoKeyManagerInterface
	userDataAttestationFile    string
	quoteFile                  string
}

// Compile time check to ensure WorkerQueues implements EspressoSubmitter
var _ EspressoSubmitter = &NitroMessageToEspressoTransactionAdapter{}

// fetchMessageForPos fetches the message at the given position from the
// message getter.
func (n *NitroMessageToEspressoTransactionAdapter) fetchMessageForPos(pos arbutil.MessageIndex) ([]byte, error) {
	msg, err := n.messageGetter.GetMessage(pos)
	if err != nil {
		return nil, err
	}
	if pos > 1 {
		prevMsg, err := n.messageGetter.GetMessage(pos - 1)
		if err != nil {
			return nil, err
		}
		if prevMsg.DelayedMessagesRead+1 == msg.DelayedMessagesRead {
			// This message is a delayed message, and it should not be included
			// in the hotshot payload. The caff node is supposed to fetch the delayed message
			// from L1.
			// setting `msg.Message` to `nil` will cause a rlp decode/encode error
			// so we set `L2msg` to an empty byte slice instead
			msg.Message.L2msg = []byte{}
		}
	}
	b, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return nil, err
	}
	return b, nil
}

type RangeInclusive[T integer] struct {
	Start, End T
}

// bundleTransactions builds an Espresso Transaction attempting to bundle as
// many messages as possible into a single transaction.
func (n *NitroMessageToEspressoTransactionAdapter) bundleTransactions(startPos, endPos arbutil.MessageIndex) (arbutil.MessageIndex, error) {
	i := startPos
	pendingTxnsPos := createSliceOfIntegerForRangeInclusive(startPos, endPos)
	for i < endPos {
		payload, msgCnt := arbutil.BuildRawHotShotPayload(pendingTxnsPos, n.fetchMessageForPos, n.espressoMaxTransactionSize)
		i += convertToUint64WithFallback[int, arbutil.MessageIndex](msgCnt, 1)
		pendingTxnsPos = pendingTxnsPos[msgCnt:]

		if msgCnt == 0 {
			return startPos, ErrorBundledHotShotTransactionContainsNoMessages{}
		}

		payload, err := arbutil.SignHotShotPayload(payload, n.keyManager.SignHotShotPayload)
		if err != nil {
			return startPos, ErrorFailedToSignDataForHotShotPayload{Cause: err}
		}

		txn := espresso_types.Transaction{
			Namespace: n.chainID,
			Payload:   payload,
		}

		n.submitter.SubmitTransaction(txn)
	}

	return endPos, nil
}

// bundleTransactionsProcess is a method that is meant to be run in a
// goroutine.
//
// It keeps track of the available positions of the transactions, as updated
// and reported by the NotifyNewPendingMessages method.
//
// Once per sending interval, it will attempt to bundle all of the available
// messages into multiple Espresso transactions, and submit them to the
// submit transaction job queue.
func (n *NitroMessageToEspressoTransactionAdapter) bundleTransactionsProcess(ctx context.Context) {
	var availablePos, submittedPos arbutil.MessageIndex
	ticker := time.NewTicker(n.sendingInterval)

	for {
		select {
		case <-ctx.Done():
			return

		case pos, ok := <-n.availableTransaction:
			if !ok {
				// The channel is closed, so we can exit
				return
			}

			if availablePos < pos {
				// We have a new available position, let's update it
				availablePos = pos
			}

		case <-ticker.C:
			// We have a ticker event, let's check if we have any pending transactions
			if availablePos == submittedPos {
				// No pending transactions, so we can skip this iteration
				continue
			}

			// Let's bundle our transactions
			nextPos, err := n.bundleTransactions(submittedPos, availablePos)
			if err != nil {
				log.Error("Failed to bundle transactions", "startPos", submittedPos, "endPos", availablePos, "error", err)
				// We can continue to the next iteration, as we will try again later
				continue
			}

			submittedPos = nextPos
		}
	}
}

// startBundleTransactionsProcess launches the bundle transactions process
// in a new goroutine using the provided stop waiter.
func (n *NitroMessageToEspressoTransactionAdapter) startBundleTransactionsProcess(sw *stopwaiter.StopWaiter) error {
	if err := sw.LaunchThreadSafe(n.bundleTransactionsProcess); err != nil {
		return ErrorFailedToLaunchBundleTransactionsProcess{Cause: err}
	}
	return nil
}

// Start implements EspressoSubmitter.
//
// This schedules the bundle transactions process to run in a new goroutine,
func (n *NitroMessageToEspressoTransactionAdapter) Start(sw *stopwaiter.StopWaiter) error {
	// start the bundle transactions process in a new goroutine
	if err := n.startBundleTransactionsProcess(sw); err != nil {
		return ErrorFailedToStart{Cause: err}
	}

	if err := n.submitter.Start(sw); err != nil {
		return err
	}

	return nil
}

// NotifyNewPendingMessages is a method that is called when new pending messages
// are available to be processed.
func (n *NitroMessageToEspressoTransactionAdapter) NotifyNewPendingMessages(pos arbutil.MessageIndex, messages []arbostypes.MessageWithMetadataAndBlockInfo) error {
	select {
	default:
		return ErrorWorkerQueuesAreFull{Pos: pos}

	case n.availableTransaction <- pos:
		return nil
	}
}

// GetKeyManager returns the key manager used by this Espresso submitter.
func (n *NitroMessageToEspressoTransactionAdapter) GetKeyManager() espresso_key_manager.EspressoKeyManagerInterface {
	return n.keyManager
}

// getAttestationQuote is a method that retrieves the attestation quote for the user data.
// This function generates the attestation quote for the user data.
// The user data is hashed using keccak256 and then 32 bytes of padding is added to the hash.
// The hash is then written to a file specified in the config. (For SGX: /dev/attestation/user_report_data)
// The quote is then read from the file specified in the config. (For SGX: /dev/attestation/quote)
func (t *NitroMessageToEspressoTransactionAdapter) getAttestationQuote(userData []byte) ([]byte, error) {
	if (t.userDataAttestationFile == "") || (t.quoteFile == "") {
		return []byte{}, nil
	}
	// keccak256 hash of userData
	userDataHash := crypto.Keccak256(userData)

	// Add 32 bytes of padding to the user data hash
	// because keccak256 hash is 32 bytes and sgx requires 64 bytes of user data
	for i := 0; i < 32; i += 1 {
		userDataHash = append(userDataHash, 0)
	}

	// Write the message to "/dev/attestation/user_report_data" in SGX
	err := os.WriteFile(t.userDataAttestationFile, userDataHash, 0600)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to create user report data file: %w", err)
	}

	// Read the quote from "/dev/attestation/quote" in SGX
	attestationQuote, err := os.ReadFile(t.quoteFile)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to read quote file: %w", err)
	}

	return attestationQuote, nil
}

// getNitroAttestation is a method that retrieves the attestation document for
// AWS Nitro Enclaves.
// This function gets the attestation document for AWS Nitro Enclaves
// We retrieve the Attestation using our epheremal public key we created in EspressoKeyManager
// After we retrieve, we verify the attestation, where we retrieve the result
// Which will contain the complete attestation which we serialize for further processing
func (t *NitroMessageToEspressoTransactionAdapter) getNitroAttestation(pubKey []byte) ([]byte, error) {
	sess, err := nsm.OpenDefaultSession()
	if err != nil {
		return nil, fmt.Errorf("failed to open nsm session: %w", err)
	}
	defer sess.Close()

	res, err := sess.Send(&request.Attestation{
		PublicKey: pubKey,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to send attestation request: %w", err)
	}

	if res.Error != "" {
		return nil, fmt.Errorf("nsm returned error: %s", res.Error)
	}

	if res.Attestation == nil || res.Attestation.Document == nil {
		return nil, fmt.Errorf("no attestation document returned")
	}

	attestation, err := nitrite.Verify(res.Attestation.Document, nitrite.VerifyOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to verify attestation")
	}

	attestationBytes, err := json.Marshal(attestation)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal attestation")
	}
	return attestationBytes, nil
}

func (n *NitroMessageToEspressoTransactionAdapter) RegisterSigner() error {
	teeType := n.keyManager.TeeType()
	switch teeType {
	case espresso_key_manager.SGX:
		return n.keyManager.Register(n.getAttestationQuote)
	case espresso_key_manager.NITRO:
		return n.keyManager.Register(n.getNitroAttestation)
	default:
		return fmt.Errorf("unsupported tee Type: %d", teeType)
	}
}

// integer represents any type that is an integer.  It specifies all types
// that are either directly integer primitives, or a type that inherits their
// functionality via wrapping.
type integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// createSliceOfIntegerForRangeInclusive creates a slice of integers from the given
// start to the end inclusive.
//
// The resulting slice will contain a sequence of integers starting at
// `start`, and ending at `endInclusive`, inclusive.
//
// Comprehension: [x | x ∈ [start, endInclusive]]
func createSliceOfIntegerForRangeInclusive[T integer](start, endInclusive T) []T {
	if start > endInclusive {
		return nil
	}

	len := int(endInclusive - start + 1)
	result := make([]T, 0, len)

	for i := start; i <= endInclusive; i++ {
		result = append(result, i)
	}

	return result
}

// ErrorWorkerQueuesAreFull is an error that is returned when the worker queues
// are full, and cannot accept new pending messages.
type ErrorWorkerQueuesAreFull struct {
	Pos arbutil.MessageIndex
}

// Error implements error
func (e ErrorWorkerQueuesAreFull) Error() string {
	return fmt.Sprintf("worker queues are full, cannot accept new pending messages at position %d", e.Pos)
}

// ErrorBundledHotShotTransactionContainsNoMessages is an error that is returned
// when a bundled hotshot transaction contains no messages. This can happen if
// a large message has exceeded the size limit or failed to get a message from
// storage.
type ErrorBundledHotShotTransactionContainsNoMessages struct{}

// Error implements error
func (e ErrorBundledHotShotTransactionContainsNoMessages) Error() string {
	return "failed to build the hotshot transaction: the result contained no messages. This can happen if a large message has exceeded the size limit or failed to get a message from storage."
}

// ErrorFailedToSignDataForHotShotPayload is an error that is returned
// when the signing of the data for the hotshot payload fails. This can happen
// if the key manager fails to sign the data, or if the data is malformed.
type ErrorFailedToSignDataForHotShotPayload struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToSignDataForHotShotPayload) Error() string {
	return fmt.Sprintf("failed to sign the data for hotshot payload: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToSignDataForHotShotPayload) Unwrap() error {
	return e.Cause
}

// ErrorFailedToLaunchBundleTransactionsProcess is an error that is returned
// when the bundle transactions process fails to launch.
type ErrorFailedToLaunchBundleTransactionsProcess struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToLaunchBundleTransactionsProcess) Error() string {
	return fmt.Sprintf("failed to launch bundle transactions process: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchBundleTransactionsProcess) Unwrap() error {
	return e.Cause
}

func (w *MultiWorkerQueueEspressoSubmitter) SubmitTransaction(txn espresso_types.Transaction) {
	// Let's submit this transaction to the job queue
	w.submitTxnsQueue <- SubmitTransactionJob{
		txn: txn,
	}
}

// ErrorFailedToLaunchSubmitTransactionQueueWorker is an error that is returned
// when the submit transaction queue worker fails to launch.
type ErrorFailedToLaunchSubmitTransactionQueueWorker struct {
	Cause  error
	Worker uint64
}

// Error implements error
func (e ErrorFailedToLaunchSubmitTransactionQueueWorker) Error() string {
	return fmt.Sprintf("failed to launch submit transaction queue worker %d: %v", e.Worker, e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchSubmitTransactionQueueWorker) Unwrap() error {
	return e.Cause
}

// ErrorFailedToLaunchSubmitTransactionScheduler is an error that is returned
// when the submit transaction scheduler fails to launch.
type ErrorFailedToLaunchSubmitTransactionScheduler struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToLaunchSubmitTransactionScheduler) Error() string {
	return fmt.Sprintf("failed to launch submit transaction scheduler: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchSubmitTransactionScheduler) Unwrap() error {
	return e.Cause
}

// ErrorFailedToLaunchSubmitTransactionResponseHandler is an error that is
// returned when the submit transaction response handler fails to launch.
type ErrorFailedToLaunchSubmitTransactionResponseHandler struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToLaunchSubmitTransactionResponseHandler) Error() string {
	return fmt.Sprintf("failed to launch submit transaction response handler: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchSubmitTransactionResponseHandler) Unwrap() error {
	return e.Cause
}

func (w *MultiWorkerQueueEspressoSubmitter) startSubmitTransactionProcess(sw *stopwaiter.StopWaiter) error {
	// Launch the workers, scheduler, and responser handler for submit transaction
	for i := uint64(0); i < w.numSubmitTransactionWorkers; i++ {
		if err := sw.LaunchThreadSafe(newSubmitTransactionWorker(i, w.client, w.submitTxnsJobQueue, w.submitTxnsResponse).startWorker); err != nil {
			return ErrorFailedToLaunchSubmitTransactionQueueWorker{Worker: i, Cause: err}
		}
	}

	if err := sw.LaunchThreadSafe(w.submitTransactionScheduler); err != nil {
		return ErrorFailedToLaunchSubmitTransactionScheduler{Cause: err}
	}

	if err := sw.LaunchThreadSafe(w.submitTransactionResponseHandler); err != nil {
		return ErrorFailedToLaunchSubmitTransactionResponseHandler{Cause: err}
	}

	return nil
}

// ErrorFailedToLaunchTransactionIncludedQueueWorker is an error that is
// returned when the transaction included queue worker fails to launch.
type ErrorFailedToLaunchTransactionIncludedQueueWorker struct {
	Cause  error
	Worker uint64
}

// Error implements error
func (e ErrorFailedToLaunchTransactionIncludedQueueWorker) Error() string {
	return fmt.Sprintf("failed to launch transaction included queue worker %d: %v", e.Worker, e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchTransactionIncludedQueueWorker) Unwrap() error {
	return e.Cause
}

// ErrorFailedToLaunchTransactionIncludedScheduler is an error that is returned
// when the transaction included scheduler fails to launch.
type ErrorFailedToLaunchTransactionIncludedScheduler struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToLaunchTransactionIncludedScheduler) Error() string {
	return fmt.Sprintf("failed to launch transaction included scheduler: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchTransactionIncludedScheduler) Unwrap() error {
	return e.Cause
}

// ErrorFailedToLaunchTransactionIncludedResponseHandler is an error that is
// returned when the transaction included response handler fails to launch.
type ErrorFailedToLaunchTransactionIncludedResponseHandler struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToLaunchTransactionIncludedResponseHandler) Error() string {
	return fmt.Sprintf("failed to launch transaction included response handler: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchTransactionIncludedResponseHandler) Unwrap() error {
	return e.Cause
}

// startTransactionIncludedProcess launches the transaction included process
// in a new goroutine using the provided stop waiter.
func (w *MultiWorkerQueueEspressoSubmitter) startTransactionIncludedProcess(sw *stopwaiter.StopWaiter) error {
	for i := uint64(0); i < w.numTransactionIncludedWorkers; i++ {
		if err := sw.LaunchThreadSafe(newTransactionIncludedQueueWorker(i, w.chainID, w.client, w.transactionIncludedJobQueue, w.transactionIncludedResponse).startWorker); err != nil {
			return ErrorFailedToLaunchTransactionIncludedQueueWorker{Worker: i, Cause: err}
		}
	}

	if err := sw.LaunchThreadSafe(w.transactionIncludedScheduler); err != nil {
		return ErrorFailedToLaunchTransactionIncludedScheduler{Cause: err}
	}

	if err := sw.LaunchThreadSafe(w.transactionIncludedResponseHandler); err != nil {
		return ErrorFailedToLaunchTransactionIncludedResponseHandler{Cause: err}
	}

	return nil
}

// ErrorFailedToStart is an error that is returned when the whole worker
// process fails to start. This can happen if any of the individual components
// fail to launch.
type ErrorFailedToStart struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToStart) Error() string {
	return fmt.Sprintf("failed to start whole worker process: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToStart) Unwrap() error {
	return e.Cause
}

// Start implements EspressoSubmitter.
func (w *MultiWorkerQueueEspressoSubmitter) Start(sw *stopwaiter.StopWaiter) error {
	w.submitTxnsJobQueue = make(chan chan SubmitTransactionJob, w.numSubmitTransactionWorkers)
	w.submitTxnsResponse = make(chan SubmitTransactionResponse, 1024)

	w.transactionIncludedJobQueue = make(chan chan TransactionIncludedJob, w.numTransactionIncludedWorkers)
	w.transactionIncludedResponse = make(chan TransactionIncludedResponse, 1024)

	// Start the submit transaction scheduler, response handlers, and workers
	if err := w.startSubmitTransactionProcess(sw); err != nil {
		return ErrorFailedToStart{Cause: err}
	}

	// Launch the workers, scheduler, and response handler for transaction inclusion
	if err := w.startTransactionIncludedProcess(sw); err != nil {
		return ErrorFailedToStart{Cause: err}
	}

	return nil
}

// SubmitTransactionJob represents a job to submit a transaction to Espresso.
type SubmitTransactionJob struct {
	txn         espresso_types.Transaction
	attempt     uint
	lastAttempt time.Time
}

// SubmitTransactionResponse represents a response / result of a performed
// SubmitTransactionJob task
type SubmitTransactionResponse struct {
	job  SubmitTransactionJob
	hash *espresso_types.TaggedBase64
	err  error
}

// submitTransactionWorker is a worker that processes SubmitTransactionJob
// tasks.
//
// It is **only** responsible for submitting transactions to Espresso, and
// returning the response back to be handled.
type submitTransactionWorker struct {
	id                 uint64
	client             espresso_client.EspressoClient
	submitTxnsJobQueue chan<- chan SubmitTransactionJob
	submitTxnsResponse chan<- SubmitTransactionResponse
}

// newSubmitTransactionWorker creates a new submit transaction worker with the
// given ID, client, job queue, and response channel.
func newSubmitTransactionWorker(id uint64, client espresso_client.EspressoClient,
	submitTxnsJobQueue chan<- chan SubmitTransactionJob,
	submitTxnsResponse chan<- SubmitTransactionResponse) *submitTransactionWorker {
	return &submitTransactionWorker{
		id:                 id,
		client:             client,
		submitTxnsJobQueue: submitTxnsJobQueue,
		submitTxnsResponse: submitTxnsResponse,
	}
}

// ErrorSubmitTransactionFailed is an error that is returned when submitting a
// transaction to Espresso fails.
type ErrorSubmitTransactionFailed struct {
	Cause         error
	WorkerID      uint64
	EstimatedHash espresso_types.TaggedBase64
}

// Error implements error
func (e ErrorSubmitTransactionFailed) Error() string {
	return fmt.Sprintf("failed to submit transaction %s: %v", e.EstimatedHash.String(), e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorSubmitTransactionFailed) Unwrap() error {
	return e.Cause
}

// startWorker is the function that actually performs the underlying work of
// the submit transaction worker.
//
// It is responsible for submitting transactions to Espresso, and handling
// the response from Espresso.
//
// NOTE: This worker does not observe the context cancellation, as it is
// expected to run until it is out of work.  Instead, it relies its job
// queue being closed by the scheduler to indicate that no more work is left
// for it to perform, then it will exit gracefully.
func (w *submitTransactionWorker) startWorker(_ context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan SubmitTransactionJob)

	for {
		// Submit our worker channel to the job queue
		w.submitTxnsJobQueue <- ch
		// Wait for a job to be sent to us
		job, ok := <-ch
		if !ok {
			log.Info("Submit transaction job queue closed, exiting", "worker", w.id)
			return
		}

		// estimate the hash
		commit := job.txn.Commit()
		estimatedHash, err := tagged_base64.New("TX", commit[:])
		if err != nil {
			// This is unfortunate, but we shouldn't stop processing the job
			// just because of a small reference failure.
			log.Info("Failed to estimate transaction hash", "error", err, "commit", common.Hash(commit), "worker", w.id)
			estimatedHash = new(tagged_base64.TaggedBase64)
		}

		// Process the job
		if job.attempt > 0 {
			// Let's slow down a little
			time.Sleep(convertToInt64WithFallback[uint, time.Duration](job.attempt, 0) * 100 * time.Millisecond)
		}

		log.Info("Submitting transaction to Espresso", "commit", common.Hash(job.txn.Commit()), "worker", w.id)

		hash, err := w.client.SubmitTransaction(ctx, job.txn)
		response := SubmitTransactionResponse{
			job:  job,
			hash: hash,
		}
		if err != nil {
			response.err = ErrorSubmitTransactionFailed{Cause: err, WorkerID: w.id, EstimatedHash: *estimatedHash}
		}

		// Send the response back to the response handler
		w.submitTxnsResponse <- response
	}
}

// TransactionIncludedJob represents a job to check if a transaction is included
// in Espresso.
type TransactionIncludedJob struct {
	txnHash        espresso_types.TaggedBase64
	txn            espresso_types.Transaction
	submitSuccess  time.Time
	submitAttempts uint
	attempt        uint
	lastAttempt    time.Time
}

// TransactionIncludedResponse represents a response / result of a performed
// TransactionIncludedJob task
type TransactionIncludedResponse struct {
	job                  TransactionIncludedJob
	transactionQueryData espresso_types.TransactionQueryData
	transactionsInBlock  espresso_client.TransactionsInBlock
	index                int
	err                  error
}

// transactionIncludedQueueWorker is a worker that processes
// TransactionIncludedJob tasks.
type transactionIncludedQueueWorker struct {
	id                          uint64
	chainID                     uint64
	client                      espresso_client.EspressoClient
	transactionIncludedJobQueue chan<- chan TransactionIncludedJob
	transactionIncludedResponse chan<- TransactionIncludedResponse
}

// newTransactionIncludedQueueWorker creates a new transaction included queue
// worker with the given ID, client, job queue, and response channel.
func newTransactionIncludedQueueWorker(id uint64, chainID uint64, client espresso_client.EspressoClient,
	transactionIncludedJobQueue chan<- chan TransactionIncludedJob,
	transactionIncludedResponse chan<- TransactionIncludedResponse) *transactionIncludedQueueWorker {
	return &transactionIncludedQueueWorker{
		id:                          id,
		chainID:                     chainID,
		client:                      client,
		transactionIncludedJobQueue: transactionIncludedJobQueue,
		transactionIncludedResponse: transactionIncludedResponse,
	}
}

// ErrorFetchTransactionByHashFailed is an error that is returned when
// fetching a transaction by its hash fails.
//
// It wraps the original error that caused the failure, and includes the
// transaction hash that was being fetched.
type ErrorFetchTransactionByHashFailed struct {
	Cause    error
	WorkerID uint64
	TxnHash  espresso_types.TaggedBase64
}

// Error implements error
func (e ErrorFetchTransactionByHashFailed) Error() string {
	return fmt.Sprintf("failed to fetch transaction by hash %s: %v", e.TxnHash.String(), e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFetchTransactionByHashFailed) Unwrap() error {
	return e.Cause
}

// ErrorFetchTransactionsForBlockAndNamespaceFailed is an error that is
// returned when fetching transactions for a specific block and namespace
// fails.
type ErrorFetchTransactionsForBlockAndNamespaceFailed struct {
	Cause       error
	WorkerID    uint64
	BlockHeight uint64
	Namespace   uint64
	TxnHash     espresso_types.TaggedBase64
}

// Error implements error
func (e ErrorFetchTransactionsForBlockAndNamespaceFailed) Error() string {
	return fmt.Sprintf("failed to fetch transactions for block %d and namespace %d, determined from transaction hash: %s: %v", e.BlockHeight, e.Namespace, e.TxnHash.String(), e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFetchTransactionsForBlockAndNamespaceFailed) Unwrap() error {
	return e.Cause
}

// ErrorTransactionNotFoundInBlock is an error that is returned when a
// transaction is not found in a specific block.
type ErrorTransactionNotFoundInBlock struct {
	WorkerID    uint64
	TxnHash     espresso_types.TaggedBase64
	BlockHeight uint64
}

// Error implements error
func (e ErrorTransactionNotFoundInBlock) Error() string {
	return fmt.Sprintf("transaction %s not found in block %d", e.TxnHash.String(), e.BlockHeight)
}

// startWorker is the function that actually performs the underlying work of
// the transaction included queue worker.
//
// It is responsible for checking if a transaction is included in Espresso,
// and handling the response from Espresso.
//
// NOTE: This worker does not observe the context cancellation, as it is
// expected to run until it is out of work.  Instead, it relies its job
// queue being closed by the worker scheduler to indicate that no more
// work is left for it to perform, then it will exit gracefully.
func (w *transactionIncludedQueueWorker) startWorker(_ context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan TransactionIncludedJob)
	for {
		// Submit our worker channel to the job queue
		w.transactionIncludedJobQueue <- ch
		// Wait for a job to be sent to us
		job, ok := <-ch
		if !ok {
			log.Info("Transaction inclusion job queue closed, exiting", "worker", w.id)
			return
		}
		// Process the job

		if job.attempt > 0 {
			// Let's slow down a little
			time.Sleep(convertToInt64WithFallback[uint, time.Duration](job.attempt, 0) * 100 * time.Millisecond)
		}

		details, err := w.client.FetchTransactionByHash(ctx, &job.txnHash)
		if err != nil {
			w.transactionIncludedResponse <- TransactionIncludedResponse{
				job: job,
				err: ErrorFetchTransactionByHashFailed{Cause: err, WorkerID: w.id, TxnHash: job.txnHash},
			}
			continue
		}

		// We need to verify the transaction in the block
		txns, err := w.client.FetchTransactionsInBlock(ctx, details.BlockHeight, job.txn.Namespace)
		if err != nil {
			w.transactionIncludedResponse <- TransactionIncludedResponse{
				job:                  job,
				transactionQueryData: details,
				err:                  ErrorFetchTransactionsForBlockAndNamespaceFailed{Cause: err, WorkerID: w.id, TxnHash: job.txnHash, BlockHeight: details.BlockHeight, Namespace: job.txn.Namespace},
			}
			continue
		}

		// One of the transactions in the block should match the txn payload
		// we submitted
		if index, isIncluded := w.doesTransactionExistInBlock(job, txns); isIncluded {
			// We found the transaction in the block, so we can mark it as included
			w.transactionIncludedResponse <- TransactionIncludedResponse{
				job:                  job,
				transactionQueryData: details,
				transactionsInBlock:  txns,
				index:                index,
			}
			continue
		}

		// If we reach here, it means the transaction was not found in
		// the block
		w.transactionIncludedResponse <- TransactionIncludedResponse{
			job:                  job,
			transactionQueryData: details,
			transactionsInBlock:  txns,
			index:                -1,
			err:                  ErrorTransactionNotFoundInBlock{WorkerID: w.id, BlockHeight: details.BlockHeight, TxnHash: job.txnHash},
		}
	}
}

// doesTransactionExistInBlock checks if the transaction exists in the block
// represented by the TransactionsInBlock object.
//
// This check is done by comparing the transaction payload with the
// transactions in the block.  If a transaction with the same payload is found,
// it is considered to be included in the block.
func (w *transactionIncludedQueueWorker) doesTransactionExistInBlock(
	job TransactionIncludedJob, txns espresso_client.TransactionsInBlock,
) (int, bool) {
	for i, txn := range txns.Transactions {
		if bytes.Equal(txn, job.txn.Payload) {
			return i, true
		}
	}
	return -1, false
}

// drainSubmitTransactionWorkers closes all the worker channels in the
// submit transaction job queue, so that the workers can exit gracefully.
func (w *MultiWorkerQueueEspressoSubmitter) drainSubmitTransactionWorkers() {
	defer close(w.submitTxnsJobQueue)

	// We need to close the submit transaction job queue, so that the workers
	// can exit gracefully.
	for i := uint64(0); i < w.numSubmitTransactionWorkers; i++ {
		worker, workerOk := <-w.submitTxnsJobQueue
		if !workerOk {
			// The job queue is closed, so we can exit.
			return
		}

		// Close the job queue of the worker, to indicate to it that
		// there are no more jobs to process
		close(worker)
	}
}

// submitTransactionScheduler is the scheduler for the submit transaction
// job queue.
func (w *MultiWorkerQueueEspressoSubmitter) submitTransactionScheduler(ctx context.Context) {
	defer w.drainSubmitTransactionWorkers()

	for {
		select {
		case <-ctx.Done():
			log.Info("Submit transaction scheduler exiting")
			return

		case job, ok := <-w.submitTxnsQueue:
			// We have a job to process, so let's get a worker, and send the
			// job to it.

			if !ok {
				log.Info("Submit transaction job queue closed, exiting")
				return
			}

			worker := <-w.submitTxnsJobQueue
			worker <- job
		}
	}
}

// submitTransactionResponseHandler is the response handler for the submit
// transaction job queue.
//
// It is responsible for determining the outcome of the performed job, and
// rescheduling the job upon failure if necessary, or moving the job to the
// transaction inclusion job queue if the job was successful.
func (w *MultiWorkerQueueEspressoSubmitter) submitTransactionResponseHandler(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Info("Submit transaction response handler exiting")
			return

		case response := <-w.submitTxnsResponse:
			// We have a response to process, so let's handle it.
			// This will likely involve updating the state of the
			// submitter, and possibly notifying other components.
			if response.err != nil || response.hash == nil {
				// Handle error case
				job := response.job
				log.Info("Failed to submit transaction to Espresso", "error", response.err, "attempts", job.attempt, "commit", common.Hash(job.txn.Commit()))
				job.attempt++
				job.lastAttempt = time.Now()
				w.submitTxnsQueue <- job
				continue
			}

			log.Info("Transaction successfully submitted to Espresso", "hash", response.hash.String(), "commit", common.Hash(response.job.txn.Commit()))

			// The job was successfully submitted, let's move to verification
			w.transactionIncludedQueue <- TransactionIncludedJob{
				txnHash:        *response.hash,
				txn:            response.job.txn,
				submitSuccess:  time.Now(),
				submitAttempts: response.job.attempt,
			}
		}
	}
}

// drainTransactionInclusionWorkers closes all the worker channels in the
// transaction inclusion job queue, so that the workers can exit gracefully.
func (w *MultiWorkerQueueEspressoSubmitter) drainTransactionInclusionWorkers() {
	defer close(w.transactionIncludedJobQueue)

	// We need to close the submit transaction job queue, so that the workers
	// can exit gracefully.
	for i := uint64(0); i < w.numTransactionIncludedWorkers; i++ {
		worker, workerOk := <-w.transactionIncludedJobQueue
		if !workerOk {
			// The job queue is closed, so we can exit.
			return
		}

		// Close the job queue of the worker, to indicate to it that
		// there are no more jobs to process
		close(worker)
	}
}

// transactionIncludedScheduler is the scheduler for the transaction inclusion
// job queue.
func (w *MultiWorkerQueueEspressoSubmitter) transactionIncludedScheduler(ctx context.Context) {
	defer w.drainTransactionInclusionWorkers()

	for {
		select {
		case <-ctx.Done():
			log.Info("Transaction inclusion scheduler exiting")
			return

		case job, ok := <-w.transactionIncludedQueue:
			if !ok {
				// The job queue is closed, so we can exit.
				log.Info("Transaction inclusion job queue closed, exiting")
				return
			}
			// We have a job to process, so let's get a worker, and send the
			// job to it.

			worker := <-w.transactionIncludedJobQueue
			worker <- job
		}
	}
}

// transactionIncludedResponseHandler is the response handler for the
// transaction inclusion job queue.
//
// It is responsible for determining the outcome of the performed job, and
// rescheduling the job upon failure if necessary.  If a transaction is taking
// too long to be included in Espresso, it will be resubmitted to the
// submit transaction job queue for another attempt.
func (w *MultiWorkerQueueEspressoSubmitter) transactionIncludedResponseHandler(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Info("Transaction inclusion response handler exiting")
			return

		case response, ok := <-w.transactionIncludedResponse:
			if !ok {
				log.Warn("Transaction inclusion response handler channel closed, exiting")
				return
			}

			if response.err != nil {
				if elapsed := time.Since(response.job.submitSuccess); elapsed > w.resubmissionDeadline {
					log.Warn("Transaction not included in Espresso within the deadline", "hash", response.job.txnHash.String(), "commit", common.Hash(response.job.txn.Commit()), "elapsed", elapsed, "attempts", response.job.attempt)
					// We have exceeded the resubmission deadline, so we, need
					// to try and resubmit the transaction.
					w.submitTxnsQueue <- SubmitTransactionJob{
						txn:     response.job.txn,
						attempt: response.job.submitAttempts + 1,
					}
					continue
				}

				log.Info("Transaction not included in Espresso", "hash", response.job.txnHash.String(), "commit", common.Hash(response.job.txn.Commit()), "attempts", response.job.attempt, "error", response.err)

				// Handle error case
				// Requeue the transaction for another inclusion check attempt
				job := response.job
				job.attempt++
				job.lastAttempt = time.Now()
				w.transactionIncludedQueue <- job
				continue
			}

			// Transaction successfully included, we have no further worker to
			// do, so we can explicitly drop the job here.

			log.Info("Transaction successfully included in Espresso", "hash", response.job.txnHash.String(), "commit", common.Hash(response.job.txn.Commit()), "attempts", response.job.attempt)
		}
	}
}
