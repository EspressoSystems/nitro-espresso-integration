package submitter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"

	tagged_base64 "github.com/EspressoSystems/espresso-network/sdks/go/tagged-base64"
	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/hf/nitrite"
	"github.com/hf/nsm"
	"github.com/hf/nsm/request"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbnode/espresso"
	espresso_key_manager "github.com/offchainlabs/nitro/arbnode/espresso/key-manager"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/util"
	"github.com/offchainlabs/nitro/util/dbutil"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

var (
	// Schema extensions for Espresso Tracking
	espressoSubmittedTxns        []byte = []byte("_espressoSubmittedTxns")    // contains the hash and pos of the submitted transactions
	espressoPendingTxnsPositions []byte = []byte("_espressoPendingTxnsPos")   // contains the index of the pending txns that need to be submitted to espresso
	espressoLastConfirmedPos     []byte = []byte("_espressoLastConfirmedPos") // contains the position of the last confirmed message
)

type EspressoOriginalSubmitter struct {
	espressoTxnsStateInsertionMutex sync.Mutex

	db                 ethdb.Database
	messageGetter      MessageGetter
	espressoClient     espresso.TransactionStreamerEspressoClient
	lightClientReader  espresso.TransactionStreamerLightClientReadeInterface
	EspressoKeyManager espresso_key_manager.EspressoKeyManagerInterface

	userDataAttestationFile string
	quoteFile               string

	chainID                               uint64
	espressoTxnsPollingInterval           time.Duration
	espressoTxnSubmissionInterval         time.Duration
	maxBlockLagBeforeEscapeHatch          uint64
	espressoMaxTransactionSize            int64
	resubmitEspressoTxDeadline            time.Duration
	lastSubmitFailureAt                   *time.Time
	UseEscapeHatch                        bool
	EscapeHatchEnabled                    bool
	InitialFinalizedSequencerMessageCount *big.Int
}

func NewEspressoOriginalSubmitter(options ...EspressoSubmitterConfigOption) (*EspressoOriginalSubmitter, error) {
	config := DefaultEspressoSubmitterConfig

	for _, option := range options {
		option(&config)
	}

	// Validate the config, to ensure we have everything we need to move
	// forward
	if err := ValidateEspressoSubmitterConfig(config); err != nil {
		return nil, fmt.Errorf("invalid espresso submitter config: %w", err)
	}

	return &EspressoOriginalSubmitter{
		db:                 config.Db,
		messageGetter:      config.MessageGetter,
		espressoClient:     config.EspressoClient,
		lightClientReader:  config.LightClientReader,
		EspressoKeyManager: config.KeyManager,

		userDataAttestationFile: config.UserDataAttestationFile,
		quoteFile:               config.QuoteFile,

		espressoTxnSubmissionInterval: config.EspressoTxnSubmissionInterval,
		espressoTxnsPollingInterval:   config.EspressoTxnsPollingInterval,
		maxBlockLagBeforeEscapeHatch:  config.MaxBlockLagBeforeEscapeHatch,
		espressoMaxTransactionSize:    config.EspressoMaxTransactionSize,
		resubmitEspressoTxDeadline:    config.ResubmitEspressoTxDeadline,
		EscapeHatchEnabled:            config.EscapeHatchEnabled,
		UseEscapeHatch:                config.UseEscapeHatch,

		InitialFinalizedSequencerMessageCount: config.InitialFinalizedSequencerMessageCount,
	}, nil
}

// Check if the latest submitted transaction has been finalized on L1 and verify it.
// Return a bool indicating whether a new transaction can be submitted to HotShot
func (s *EspressoOriginalSubmitter) checkSubmittedTransactionForFinality(ctx context.Context) error {
	s.espressoTxnsStateInsertionMutex.Lock()
	defer s.espressoTxnsStateInsertionMutex.Unlock()

	submittedTxns, err := s.getEspressoSubmittedTxns()
	if err != nil {
		return fmt.Errorf("submitted transactions not found: %w", err)
	}
	if len(submittedTxns) == 0 {
		return nil // no submitted transaction, treated as successful
	}

	batch := s.db.NewBatch()
	newSubmittedTxns := []arbutil.SubmittedEspressoTx{}
	lastConfirmedPos := arbutil.MessageIndex(0)
	for _, submittedTx := range submittedTxns {
		hash := submittedTx.Hash
		submittedTxHash, err := tagged_base64.Parse(hash)
		if err != nil || submittedTxHash == nil {
			return fmt.Errorf("invalid hotshot tx hash, failed to parse hash %s: %w", hash, err)
		}

		data, err := s.checkEspressoQueryNodesForTransaction(ctx, submittedTxHash)
		if err != nil {
			resubmittedTxn, err := s.resubmitTransactionIfPastDelay(ctx, submittedTx)
			if err != nil {
				log.Error("failed to resubmit transaction", "err", err)
				continue
			}
			if resubmittedTxn != nil {
				newSubmittedTxns = append(newSubmittedTxns, *resubmittedTxn)
			} else {
				newSubmittedTxns = append(newSubmittedTxns, submittedTx)
			}
			continue
		}

		height := data.BlockHeight

		resp, err := s.espressoClient.FetchTransactionsInBlock(ctx, height, s.chainID)
		if err != nil {
			log.Warn("Failed to fetch transactions in block referenced in fetch transaction by hash", "height", height, "error", err)
			continue
		}

		validated := arbutil.ValidateIfPayloadIsInBlock(submittedTx.Payload, resp.Transactions)
		if !validated {
			log.Warn("Transaction payload not found in block, attempt to resubmit", "height", height, "tx", submittedTx.Hash)
			resubmittedTxn, err := s.resubmitTransaction(ctx, submittedTx)
			if err != nil {
				log.Error("failed to resubmit transaction", "err", err)
				continue
			}
			if resubmittedTxn == nil {
				// This should never happen
				log.Error("failed to resubmit transaction", "err", err)
				continue
			}
			newSubmittedTxns = append(newSubmittedTxns, *resubmittedTxn)
			continue
		}

		if submittedTx.Pos[len(submittedTx.Pos)-1] > lastConfirmedPos {
			lastConfirmedPos = submittedTx.Pos[len(submittedTx.Pos)-1]
		}

	}

	err = s.setEspressoLastConfirmedPos(batch, &lastConfirmedPos)
	if err != nil {
		return fmt.Errorf("failed to set last confirmed pos: %w", err)
	}

	// this will be remmoved in other PRs
	err = s.setEspressoSubmittedTxns(batch, newSubmittedTxns)
	if err != nil {
		return fmt.Errorf("failed to set espresso submitted txns: %w", err)
	}

	if err = batch.Write(); err != nil {
		return fmt.Errorf("failed to write to db: %w", err)
	}

	return nil
}

// Check if the latest submitted transaction has been finalized on L1 and verify it.
func (s *EspressoOriginalSubmitter) checkEspressoQueryNodesForTransaction(ctx context.Context, hash *tagged_base64.TaggedBase64) (espresso_types.TransactionQueryData, error) {
	tx, err := s.espressoClient.FetchTransactionByHash(ctx, hash)
	if err != nil {
		return espresso_types.TransactionQueryData{}, fmt.Errorf("failed to fetch transaction from espresso: %w", err)
	}

	return tx, nil
}

func (s *EspressoOriginalSubmitter) resubmitTransaction(ctx context.Context, submittedTx arbutil.SubmittedEspressoTx) (*arbutil.SubmittedEspressoTx, error) {
	submittedAt := time.Now()
	hash, err := s.espressoClient.SubmitTransaction(ctx, espresso_types.Transaction{
		Payload:   submittedTx.Payload,
		Namespace: s.chainID,
	})
	if err != nil {
		return nil, err
	}
	submittedTx.Hash = hash.String()
	submittedTx.SubmittedAt = submittedAt
	return &submittedTx, nil
}

func (s *EspressoOriginalSubmitter) resubmitTransactionIfPastDelay(ctx context.Context, submittedTx arbutil.SubmittedEspressoTx) (*arbutil.SubmittedEspressoTx, error) {
	timeSinceSubmission := time.Since(submittedTx.SubmittedAt)
	if timeSinceSubmission < s.resubmitEspressoTxDeadline {
		return nil, nil
	}
	return s.resubmitTransaction(ctx, submittedTx)
}

func (s *EspressoOriginalSubmitter) getEspressoSubmittedTxns() ([]arbutil.SubmittedEspressoTx, error) {
	posBytes, err := s.db.Get(espressoSubmittedTxns)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var tx []arbutil.SubmittedEspressoTx
	err = rlp.DecodeBytes(posBytes, &tx)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

func (s *EspressoOriginalSubmitter) getLastConfirmedPos() (*arbutil.MessageIndex, error) {
	lastConfirmedBytes, err := s.db.Get(espressoLastConfirmedPos)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var lastConfirmed arbutil.MessageIndex
	err = rlp.DecodeBytes(lastConfirmedBytes, &lastConfirmed)
	if err != nil {
		return nil, err
	}
	return &lastConfirmed, nil
}

func (s *EspressoOriginalSubmitter) getEspressoPendingTxnsPos() ([]arbutil.MessageIndex, error) {

	pendingTxnsBytes, err := s.db.Get(espressoPendingTxnsPositions)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var pendingTxnsPos []arbutil.MessageIndex
	err = rlp.DecodeBytes(pendingTxnsBytes, &pendingTxnsPos)
	if err != nil {
		return nil, err
	}
	return pendingTxnsPos, nil
}

func (s *EspressoOriginalSubmitter) setEspressoSubmittedTxns(batch ethdb.KeyValueWriter, txns []arbutil.SubmittedEspressoTx) error {
	// if pos is nil, delete the key
	if txns == nil {
		err := batch.Delete(espressoSubmittedTxns)
		return err
	}

	bytes, err := rlp.EncodeToBytes(txns)
	if err != nil {
		return err
	}
	err = batch.Put(espressoSubmittedTxns, bytes)
	if err != nil {
		return err
	}

	return nil
}

func (s *EspressoOriginalSubmitter) setEspressoLastConfirmedPos(batch ethdb.KeyValueWriter, pos *arbutil.MessageIndex) error {
	posBytes, err := rlp.EncodeToBytes(pos)
	if err != nil {
		return err
	}
	err = batch.Put(espressoLastConfirmedPos, posBytes)
	if err != nil {
		return err

	}
	return nil
}

func (s *EspressoOriginalSubmitter) setEspressoPendingTxnsPos(batch ethdb.KeyValueWriter, pos []arbutil.MessageIndex) error {
	if pos == nil {
		err := batch.Delete(espressoPendingTxnsPositions)
		return err
	}

	posBytes, err := rlp.EncodeToBytes(pos)
	if err != nil {
		return err
	}
	err = batch.Put(espressoPendingTxnsPositions, posBytes)
	if err != nil {
		return err

	}
	return nil
}

func (s *EspressoOriginalSubmitter) NotifyNewPendingMessages(pos arbutil.MessageIndex, messages []arbostypes.MessageWithMetadataAndBlockInfo) error {
	//  Only submit the transaction if escape hatch is not enabled
	for i := range messages {
		// This is a safe cast, as we're going from `int` to `uint64`.
		// Unless we were going from `int128` (which doesn't currently
		// exist in Go) to `uint64`, this will always succeed.
		idx := uint64(i)
		indexToSubmit := (pos + arbutil.MessageIndex(idx))

		// convert to uint64
		if s.shouldSubmitEspressoTransaction(uint64(indexToSubmit)) {
			log.Info("Enqueuing pending transaction to Espresso", "pos", pos+arbutil.MessageIndex(idx))
			// Store the pos in the database to be used later to submit the message
			// to hotshot for finalization.
			err := s.SubmitEspressoTransactionPos(pos + +arbutil.MessageIndex(idx))
			if err != nil {
				log.Error("Failed to enqueue pending transaction to Espresso", "pos", pos+arbutil.MessageIndex(idx), "err", err)
				return err
			}
			log.Info("Enqueued pending transaction to Espresso was successful", "pos", pos+arbutil.MessageIndex(idx))
		}

	}

	return nil
}

// Append a position to the pending queue. Please ensure this position is valid beforehand.
func (s *EspressoOriginalSubmitter) SubmitEspressoTransactionPos(pos arbutil.MessageIndex) error {
	s.espressoTxnsStateInsertionMutex.Lock()
	defer s.espressoTxnsStateInsertionMutex.Unlock()

	batch := s.db.NewBatch()
	pendingTxnsPos, err := s.getEspressoPendingTxnsPos()
	if err != nil {
		return err
	}

	if pendingTxnsPos == nil {
		// if the key doesn't exist, create a new array with the pos
		pendingTxnsPos = []arbutil.MessageIndex{pos}
	} else {
		pendingTxnsPos = append(pendingTxnsPos, pos)
	}
	err = s.setEspressoPendingTxnsPos(batch, pendingTxnsPos)
	if err != nil {
		log.Error("failed to set the pending txns", "err", err)
		return err
	}

	err = batch.Write()
	if err != nil {
		return err
	}

	return nil
}

func (s *EspressoOriginalSubmitter) ResubmitEspressoTransactions(ctx context.Context, tx arbutil.SubmittedEspressoTx) (*tagged_base64.TaggedBase64, error) {
	txHash, err := s.espressoClient.SubmitTransaction(ctx, espresso_types.Transaction{
		Payload:   tx.Payload,
		Namespace: s.chainID,
	})
	if err != nil {
		return nil, err
	}

	return txHash, nil
}

func (s *EspressoOriginalSubmitter) submitEspressoTransactions(ctx context.Context) error {
	s.espressoTxnsStateInsertionMutex.Lock()
	defer s.espressoTxnsStateInsertionMutex.Unlock()
	pendingTxnsPos, err := s.getEspressoPendingTxnsPos()
	if err != nil {
		return err
	}

	if len(pendingTxnsPos) <= 0 {
		// Nothing to submit
		return nil
	}

	fetcher := func(pos arbutil.MessageIndex) ([]byte, error) {
		msg, err := s.messageGetter.GetMessage(pos)
		if err != nil {
			return nil, err
		}
		if pos > 1 {
			prevMsg, err := s.messageGetter.GetMessage(pos - 1)
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

	payload, msgCnt := arbutil.BuildRawHotShotPayload(pendingTxnsPos, fetcher, s.espressoMaxTransactionSize)
	if msgCnt == 0 {
		return fmt.Errorf("failed to build the hotshot transaction: a large message has exceeded the size limit or failed to get a message from storage")
	}

	payload, err = arbutil.SignHotShotPayload(payload, s.EspressoKeyManager.SignHotShotPayload)
	if err != nil {
		return fmt.Errorf("failed to sign the hotshot payload %w", err)
	}

	log.Info("submitting transaction to hotshot for finalization")

	submittedAt := time.Now()
	// Note: same key should not be used for two namespaces for this to work
	hash, err := s.espressoClient.SubmitTransaction(ctx, espresso_types.Transaction{
		Payload:   payload,
		Namespace: s.chainID,
	})

	if err != nil {
		return fmt.Errorf("failed to submit transaction to espresso: %w", err)
	}

	batch := s.db.NewBatch()
	submittedPos := pendingTxnsPos[:msgCnt]

	submittedTxns, err := s.getEspressoSubmittedTxns()
	if err != nil {
		return fmt.Errorf("failed to get the submitted txns: %w", err)
	}
	tx := arbutil.SubmittedEspressoTx{
		Hash:        hash.String(),
		Pos:         submittedPos,
		Payload:     payload,
		SubmittedAt: submittedAt,
	}
	if submittedTxns == nil {
		submittedTxns = []arbutil.SubmittedEspressoTx{tx}
	} else {
		submittedTxns = append(submittedTxns, tx)
	}

	if err = s.setEspressoSubmittedTxns(batch, submittedTxns); err != nil {
		return fmt.Errorf("failed to set espresso submitted txns: %w", err)
	}

	pendingTxnsPos = pendingTxnsPos[msgCnt:]
	err = s.setEspressoPendingTxnsPos(batch, pendingTxnsPos)
	if err != nil {
		return fmt.Errorf("failed to set the pending txn: %w", err)
	}

	err = batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write to db: %w", err)
	}

	return nil
}

// Make sure useEscapeHatch is true
func (s *EspressoOriginalSubmitter) checkEspressoLiveness() error {
	live, err := s.lightClientReader.IsHotShotLive(s.maxBlockLagBeforeEscapeHatch)
	if err != nil {
		return err
	}
	// If escape hatch is activated, the only thing is to check if hotshot is live again
	if s.EscapeHatchEnabled {
		if live {
			log.Info("HotShot is up, disabling the escape hatch")
			s.EscapeHatchEnabled = false
		}
		return nil
	}

	// If escape hatch is disabled, hotshot is live, everything is fine
	if live {
		return nil
	}

	// If escape hatch is on, and hotshot is down
	log.Warn("enabling the escape hatch, hotshot is down")
	s.EscapeHatchEnabled = true

	return nil
}

var ErrEspressoValidation = errors.New("failed to check espresso validation")
var ErrEspressoFetchTransaction = errors.New("failed to fetch the espresso transaction")

var espressoMerkleProofEphemeralErrorHandler = util.NewEphemeralErrorHandler(80*time.Minute, ErrEspressoValidation.Error(), 15*time.Minute)
var espressoTransactionEphemeralErrorHandler = util.NewEphemeralErrorHandler(3*time.Minute, ErrEspressoFetchTransaction.Error(), 15*time.Minute)

func getLogLevel(err error) func(string, ...interface{}) {
	logLevel := log.Error
	logLevel = espressoMerkleProofEphemeralErrorHandler.LogLevel(err, logLevel)
	logLevel = espressoTransactionEphemeralErrorHandler.LogLevel(err, logLevel)
	return logLevel
}

// pollSubmittedTransactionForFinality checks if the submitted transaction has
// been finalized by Espresso  and verifies it.
func (s *EspressoOriginalSubmitter) pollSubmittedTransactionForFinality(ctx context.Context, ignored struct{}) time.Duration {
	retryRate := s.espressoTxnsPollingInterval * 2
	var err error
	if s.UseEscapeHatch {
		err = s.checkEspressoLiveness()
		if err != nil {
			if ctx.Err() != nil {
				return s.espressoTxnsPollingInterval
			}
			logLevel := getLogLevel(err)
			logLevel("error checking escape hatch, will retry", "err", err)
			return retryRate
		}
		espressoTransactionEphemeralErrorHandler.Reset()
	}
	err = s.checkSubmittedTransactionForFinality(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return s.espressoTxnsPollingInterval
		}
		logLevel := getLogLevel(err)
		logLevel("error polling finality, will retry", "err", err)
		return retryRate
	}
	espressoMerkleProofEphemeralErrorHandler.Reset()
	return s.espressoTxnsPollingInterval
}

// submitTransactionsToEspresso submits the transactions to espresso if the
// escape hatch is not enabled
func (s *EspressoOriginalSubmitter) submitTransactionsToEspresso(ctx context.Context, ignored struct{}) time.Duration {
	// Only submit the transaction if escape hatch is not enabled
	if s.EscapeHatchEnabled {
		return s.espressoTxnsPollingInterval
	}

	if err := s.submitEspressoTransactions(ctx); err != nil {
		log.Error("failed to submit espresso transactions", "err", err)
		// When encountering an error during the initial attempt at submitting a transaction, double the amount of our polling interval and try again.
		return s.espressoTxnsPollingInterval * 2
	}

	return s.espressoTxnsPollingInterval
}

func (s *EspressoOriginalSubmitter) pollToResubmitEspressoTransactions(ctx context.Context, ignored struct{}) time.Duration {
	retryRate := s.espressoTxnSubmissionInterval * 2
	submittedTxns, err := s.getEspressoSubmittedTxns()
	if err != nil {
		log.Warn("resubmitting espresso transactions failed: unable to get submitted transactions, will retry: %w", err)
		return retryRate
	}

	shouldResubmit := s.shouldResubmitEspressoTransactions(ctx, submittedTxns)
	if shouldResubmit {
		for _, tx := range submittedTxns {
			log.Info("Resubmitting tx to Espresso", "tx", tx.Hash)
			txHash, err := s.ResubmitEspressoTransactions(ctx, tx)
			if err != nil {
				log.Warn("failed to resubmit espresso transactions", "err", err)
				return retryRate
			}
			log.Info(fmt.Sprintf("trying to resubmit transaction succeeded: (hash: %s)", txHash.String()))
		}
		// Reset the last submit failure time because we successfully resubmitted the transactions
		s.lastSubmitFailureAt = nil
	}
	return s.espressoTxnSubmissionInterval
}

// shouldSubmitEspressoTransaction is a method that checks the conditions under
// which we are able to submit transactions to Espresso.  If these conditions
// are not met, we will not submit the transaction to Espresso.
//
// The necessary conditions are:
//   - The Espresso Client and Light Client Reader must be set
//   - The given `pos` parameter must be after our recorded finalized sequencer
//     message count
//   - The Escape Hatch must not be enabled
//
// NOTE: This method does not acquire any locks, so its state may change
// when running concurrently with other methods.
func (s *EspressoOriginalSubmitter) shouldSubmitEspressoTransaction(pos uint64) bool {
	if p := pos; p < s.InitialFinalizedSequencerMessageCount.Uint64() {
		log.Warn("not submitting transaction to espresso due to it being finalized", "pos", p, "sequencerMessageCount", s.InitialFinalizedSequencerMessageCount)
		return false
	}

	return !s.EscapeHatchEnabled
}

func (s *EspressoOriginalSubmitter) shouldResubmitEspressoTransactions(ctx context.Context, submittedTxns []arbutil.SubmittedEspressoTx) bool {
	if len(submittedTxns) == 0 {
		// If no submitted transactions, we dont need to resubmit
		return false
	}
	firstSubmitted := submittedTxns[0]
	hash := firstSubmitted.Hash

	submittedTxHash, err := tagged_base64.Parse(hash)
	if err != nil || submittedTxHash == nil {
		log.Error("invalid hotshot tx hash, failed to parse hash %s: %w", hash, err)
		return false
	}

	_, err = s.espressoClient.FetchTransactionByHash(ctx, submittedTxHash)
	if err == nil {
		// if we are able to fetch the transaction, we dont need to resubmit
		return false
	}

	if s.lastSubmitFailureAt == nil {
		now := time.Now()
		s.lastSubmitFailureAt = &now
		log.Warn("will wait for resubmission deadline before resubmitting transaction, will retry again", "hash", submittedTxHash.String(), "err", err)
		return false
	}
	duration := time.Since(*s.lastSubmitFailureAt)
	if duration < s.resubmitEspressoTxDeadline {
		log.Warn("resubmission deadline not reached, will retry again", "hash", submittedTxHash.String(), "err", err)
		return false
	}

	return true
}

func (s *EspressoOriginalSubmitter) RegisterSigner() error {
	teeType := s.EspressoKeyManager.TeeType()
	switch teeType {
	case espresso_key_manager.SGX:
		return s.EspressoKeyManager.Register(s.getAttestationQuote)
	case espresso_key_manager.NITRO:
		return s.EspressoKeyManager.Register(s.getNitroAttestation)
	default:
		return fmt.Errorf("unsupported tee Type: %d", teeType)
	}
}

func (s *EspressoOriginalSubmitter) Start(sw *stopwaiter.StopWaiter) error {
	if s.lightClientReader != nil && s.espressoClient != nil {
		err := s.RegisterSigner()
		if err != nil {
			log.Error("failed to register espresso key manager", "err", err)
			return err
		}
		err = stopwaiter.CallIterativelyWith[struct{}](sw, s.pollSubmittedTransactionForFinality, nil)
		if err != nil {
			return err
		}
		err = stopwaiter.CallIterativelyWith[struct{}](sw, s.submitTransactionsToEspresso, nil)
		if err != nil {
			return err
		}
		err = stopwaiter.CallIterativelyWith[struct{}](sw, s.pollToResubmitEspressoTransactions, nil)
		if err != nil {
			return err
		}
	} else {
		log.Warn("light client reader or espresso client not set, skipping espresso verification")
	}

	return nil
}

// getAttestationQuote is a method that retrieves the attestation quote for the user data.
// This function generates the attestation quote for the user data.
// The user data is hashed using keccak256 and then 32 bytes of padding is added to the hash.
// The hash is then written to a file specified in the config. (For SGX: /dev/attestation/user_report_data)
// The quote is then read from the file specified in the config. (For SGX: /dev/attestation/quote)
func (t *EspressoOriginalSubmitter) getAttestationQuote(userData []byte) ([]byte, error) {

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
func (t *EspressoOriginalSubmitter) getNitroAttestation(pubKey []byte) ([]byte, error) {

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

func (t *EspressoOriginalSubmitter) GetKeyManager() espresso_key_manager.EspressoKeyManagerInterface {
	return t.EspressoKeyManager
}

func (t *EspressoOriginalSubmitter) IsEscapeHatchEnabled() bool {
	return t.EscapeHatchEnabled
}

func (t *EspressoOriginalSubmitter) GetLastConfirmedPosition() (*arbutil.MessageIndex, error) {
	return t.getLastConfirmedPos()
}
