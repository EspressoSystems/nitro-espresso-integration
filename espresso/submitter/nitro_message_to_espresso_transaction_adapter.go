package submitter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/hf/nitrite"
	"github.com/hf/nsm"
	"github.com/hf/nsm/request"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	espresso_key_manager "github.com/offchainlabs/nitro/espresso/key-manager"
	"github.com/offchainlabs/nitro/util"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

// NitroMessageToEspressoTransactionAdapter is an implementation of
// EspressoSubmitter that adapts Nitro messages to Espresso transactions.
type NitroMessageToEspressoTransactionAdapter struct {
	db                           ethdb.Database
	chainID                      uint64
	submitter                    *MultiWorkerQueueEspressoSubmitter
	availableTransaction         chan arbutil.MessageIndex
	messageGetter                MessageGetter
	espressoMaxTransactionSize   int64
	sendingInterval              time.Duration
	keyManager                   espresso_key_manager.EspressoKeyManagerInterface
	userDataAttestationFile      string
	quoteFile                    string
	initialNitroMessageAvailable arbutil.MessageIndex
	initialNitroMessageToSubmit  arbutil.MessageIndex
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

// bundleTransactions builds an Espresso Transaction attempting to bundle as
// many messages as possible into a single transaction.
func (n *NitroMessageToEspressoTransactionAdapter) bundleTransactions(startPos, endPos arbutil.MessageIndex) (arbutil.MessageIndex, error) {
	i := startPos
	pendingTxnsPos := util.CreateSliceOfIntegerForRangeInclusive(startPos, endPos)
	for i <= endPos {
		payload, msgCnt := arbutil.BuildRawHotShotPayload(pendingTxnsPos, n.fetchMessageForPos, n.espressoMaxTransactionSize)
		i += util.ConvertToUint64WithFallback[int, arbutil.MessageIndex](msgCnt, 1)
		pendingTxnsPos = pendingTxnsPos[msgCnt:]

		if msgCnt == 0 {
			return startPos, ErrorBundledHotShotTransactionContainsNoMessages{}
		}

		payload, err := arbutil.SignHotShotPayload(payload, n.keyManager.SignPayload)
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
	availablePos, submittedPos := n.initialNitroMessageAvailable, n.initialNitroMessageToSubmit
	ticker := time.NewTicker(n.sendingInterval)

	for {
		select {
		case <-ctx.Done():
			log.Warn("Bundle transactions process exiting, due to context done", "error", ctx.Err())
			return

		case pos, ok := <-n.availableTransaction:
			if !ok {
				log.Warn("Available transaction channel closed, exiting bundle transactions process")
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
		return nil, fmt.Errorf("failed to verify attestation: %w", err)
	}

	attestationBytes, err := json.Marshal(attestation)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal attestation")
	}
	return attestationBytes, nil
}

func (n *NitroMessageToEspressoTransactionAdapter) Init() error {
	teeType := n.keyManager.TeeType()
	switch teeType {
	case espresso_key_manager.SGX:
		return n.keyManager.InitRegistration(n.getAttestationQuote)
	case espresso_key_manager.NITRO:
		return n.keyManager.InitRegistration(n.getNitroAttestation)
	case espresso_key_manager.TESTS:
		return n.keyManager.InitRegistration(n.getAttestationQuote)
	default:
		return fmt.Errorf("unsupported tee Type: %d", teeType)
	}
}

func (n *NitroMessageToEspressoTransactionAdapter) RegisterService() error {
	return n.keyManager.RegisterService()
}

func (n *NitroMessageToEspressoTransactionAdapter) EnqueuePendingTransaction(pos []arbutil.MessageIndex) error {
	return nil
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
