package espressostreamer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	espressoClient "github.com/EspressoSystems/espresso-network/sdks/go/client"
	espressoTypes "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/ccoveille/go-safecast"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	decentralized_timeboost "github.com/offchainlabs/nitro/decentralized-timeboost/helpers"
	"github.com/offchainlabs/nitro/espressotee"
	"github.com/offchainlabs/nitro/solgen/go/decentralizedtimeboostgen"
	"github.com/offchainlabs/nitro/util"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

var (
	ErrFailedToFetchTransactions  = errors.New("failed to fetch transactions")
	ErrPayloadHadNoMessages       = errors.New("ParseHotShotPayload found no messages, the transaction may be empty")
	ErrUserDataHashNot32Bytes     = errors.New("user data hash is not 32 bytes")
	ErrRetryParsingHotShotPayload = errors.New("failed to parse hotshot payload, but need retry")
)

type EspressoStreamerInterface interface {
	Start(ctx context.Context) error
	Next() *MessageWithMetadataAndPos
	// Peek returns the next message in the streamer's buffer. If the message is not
	// in the buffer, it will return nil.
	Peek() *MessageWithMetadataAndPos
	// Advance moves the current message position to the next message.
	Advance()
	// Reset sets the current message position and the next hotshot block number.
	Reset(currentMessagePos uint64, currentHostshotBlock uint64)
	// RecordTimeDurationBetweenHotshotAndCurrentBlock records the time duration between
	// the next hotshot block and the current block.
	RecordTimeDurationBetweenHotshotAndCurrentBlock(nextHotshotBlock uint64, blockProductionTime time.Time)
	GetCurrentEarliestHotShotBlockNumber() uint64

	SetBatcherAddressesFetcher(fetcher func(l1Height uint64) []common.Address)
	StopAndWait()
}

type MessageWithMetadataAndPos struct {
	MessageWithMeta arbostypes.MessageWithMetadata
	Pos             uint64
	HotshotHeight   uint64
}

type EspressoStreamer struct {
	stopwaiter.StopWaiter
	espressoClient            espressoClient.EspressoClient
	nextHotshotBlockNum       uint64
	currentMessagePos         uint64
	namespace                 uint64
	messageWithMetadataAndPos map[uint64]*MessageWithMetadataAndPos
	espressoSGXVerifier       espressotee.EspressoSGXVerifierInterface

	messageLock sync.RWMutex
	retryTime   time.Duration

	PerfRecorder *PerfRecorder

	batcherAddressesFetcher  func(l1Height uint64) []common.Address
	isDecentralizedTimeboost bool
	committeeFetcher         func(opts *bind.CallOpts, id uint64) (decentralizedtimeboostgen.KeyManagerCommittee, error)

	dangerousMinimumHotshotBlockNum uint64
	highestPos                      uint64
}

var _ EspressoStreamerInterface = (*EspressoStreamer)(nil)

func NewEspressoStreamer(
	namespace uint64,
	nextHotshotBlockNum uint64,
	espressoSGXVerifier espressotee.EspressoSGXVerifierInterface,
	espressoClient espressoClient.EspressoClient,
	recordPerformance bool,
	batcherAddressesFetcher func(l1Height uint64) []common.Address,
	retryTime time.Duration,
	isDecentralizedTimeboost bool,
	committeeFetcher func(opts *bind.CallOpts, id uint64) (decentralizedtimeboostgen.KeyManagerCommittee, error),
	dangerousMinimumHotshotBlockNum uint64,
) *EspressoStreamer {

	var PerfRecorder *PerfRecorder
	if recordPerformance {
		PerfRecorder = NewPerfRecorder()
	}

	return &EspressoStreamer{
		espressoClient:                  espressoClient,
		nextHotshotBlockNum:             nextHotshotBlockNum,
		namespace:                       namespace,
		espressoSGXVerifier:             espressoSGXVerifier,
		PerfRecorder:                    PerfRecorder,
		batcherAddressesFetcher:         batcherAddressesFetcher,
		retryTime:                       retryTime,
		currentMessagePos:               1,
		isDecentralizedTimeboost:        isDecentralizedTimeboost,
		committeeFetcher:                committeeFetcher,
		dangerousMinimumHotshotBlockNum: dangerousMinimumHotshotBlockNum,
		messageWithMetadataAndPos:       make(map[uint64]*MessageWithMetadataAndPos),
	}
}

// GetMessageCount
// This function will use the CountUniqueMessage to count the unique messages present in it's buffer.
// Parameters:
//
//	None
//
// Return value:
//
//	a uint64 representing the estimated message count.
func (s *EspressoStreamer) GetMessageCount() uint64 {
	s.messageLock.RLock()
	defer s.messageLock.RUnlock()
	count := uint64(0)
	end := s.currentMessagePos + uint64(len(s.messageWithMetadataAndPos))
	for start := s.currentMessagePos; start <= end; start++ {
		if _, ok := s.messageWithMetadataAndPos[start]; !ok {
			break
		}
		count += 1
	}
	return s.currentMessagePos + count
}

func (s *EspressoStreamer) GetCurrentMessagePosition() uint64 {
	s.messageLock.RLock()
	defer s.messageLock.RUnlock()

	return s.currentMessagePos
}

func (s *EspressoStreamer) Reset(currentMessagePos uint64, currentHostshotBlock uint64) {
	s.messageLock.Lock()
	defer s.messageLock.Unlock()

	hotshotBlockNum := currentHostshotBlock
	if currentHostshotBlock < s.dangerousMinimumHotshotBlockNum {
		hotshotBlockNum = s.dangerousMinimumHotshotBlockNum
	}

	s.currentMessagePos = currentMessagePos
	s.nextHotshotBlockNum = hotshotBlockNum
	s.messageWithMetadataAndPos = make(map[uint64]*MessageWithMetadataAndPos)
}

func (s *EspressoStreamer) Next() *MessageWithMetadataAndPos {
	result := s.Peek()
	if result == nil {
		return nil
	}

	// Advance the current message position, so that the next call to
	// `Peek` or `Next` will return the next message
	s.Advance()
	return result
}

func (s *EspressoStreamer) GetMsg(pos arbutil.MessageIndex) *MessageWithMetadataAndPos {
	s.messageLock.RLock()
	defer s.messageLock.RUnlock()

	return s.messageWithMetadataAndPos[uint64(pos)]
}

func (s *EspressoStreamer) Peek() *MessageWithMetadataAndPos {
	s.messageLock.RLock()
	defer s.messageLock.RUnlock()

	return s.messageWithMetadataAndPos[s.currentMessagePos]
}

// Checks if we have a consecutive sequence of messages from the current position to the target.
// This is used when verifying correctness of batch sent from another batch poster for decentralized timeboost
// We need to be sure the batch isnt lying about the espresso confirmations so we verify against what we have in our internal state
// Return the minimum hotshot position after the target for when we call `Reset()` on the streamer to ensure no data will be lost
func (s *EspressoStreamer) VerifyConsecutivePositions(start uint64, target uint64) *uint64 {
	s.messageLock.RLock()
	defer s.messageLock.RUnlock()

	if start <= s.currentMessagePos {
		start = s.currentMessagePos
	}

	// Verify all positions exist from start to target
	foundAll := true
	for pos := start; pos < target; pos++ {
		if _, exists := s.messageWithMetadataAndPos[pos]; !exists {
			foundAll = false
			break
		}
	}

	if foundAll {
		// Get pre-computed min height
		pos := s.messageWithMetadataAndPos[target-1]
		return &pos.HotshotHeight
	}
	log.Warn(
		"failed to verify consecutive position in streamer",
		"prevMsgCount", start,
		"newMsgCount", target,
		"streamerPos", s.currentMessagePos,
		"hotshot block", s.GetCurrentEarliestHotShotBlockNumber(),
		"len", len(s.messageWithMetadataAndPos),
	)
	return nil
}

// Call this function to advance the streamer to the next message
func (s *EspressoStreamer) Advance() {
	s.messageLock.Lock()
	defer s.messageLock.Unlock()
	delete(s.messageWithMetadataAndPos, s.currentMessagePos)
	s.currentMessagePos += 1
}

func (s *EspressoStreamer) AdvanceTo(msg uint64) {
	s.messageLock.Lock()
	defer s.messageLock.Unlock()
	if msg <= s.currentMessagePos {
		return
	}

	for pos := s.currentMessagePos; pos < msg; pos++ {
		delete(s.messageWithMetadataAndPos, pos)
	}

	s.currentMessagePos = msg
}

// This function keep fetching hotshot blocks and parsing them until the condition is met.
// It is a do-while loop, which means it will always execute at least once.
//
// Expose the *parseHotShotPayloadFn* to the caller for testing purposes
func (s *EspressoStreamer) QueueMessagesFromHotshot(
	ctx context.Context,
	parseHotShotPayloadFn func(tx espressoTypes.Bytes, l1Height uint64) ([]*MessageWithMetadataAndPos, error),
) error {
	messages, err := fetchNextHotshotBlock(
		ctx,
		s.espressoClient,
		s.nextHotshotBlockNum,
		parseHotShotPayloadFn,
		s.namespace,
	)
	if err != nil {
		return err
	}

	s.messageLock.Lock()
	defer s.messageLock.Unlock()
	for _, msg := range messages {
		if msg.Pos < s.currentMessagePos {
			log.Debug("message index is less than current message pos, skipping", "msgPos", msg.Pos, "currentMessagePos", s.currentMessagePos)
		}
		// in the case a transaction was resubmitted we dont need to re add the position
		if _, ok := s.messageWithMetadataAndPos[msg.Pos]; ok {
			continue
		}

		s.messageWithMetadataAndPos[msg.Pos] = msg

		if msg.Pos > s.highestPos {
			s.highestPos = msg.Pos
		}

		// Check if we have a higher position in an earlier block
		currHeight := msg.HotshotHeight
		for nextPos := msg.Pos + 1; nextPos <= s.highestPos; nextPos++ {
			if higherPos, ok := s.messageWithMetadataAndPos[nextPos]; ok && higherPos.HotshotHeight < currHeight {
				s.messageWithMetadataAndPos[msg.Pos].HotshotHeight = higherPos.HotshotHeight
			}
		}
	}
	s.nextHotshotBlockNum += 1
	return nil
}

func (s *EspressoStreamer) verifyBatchPosterSignature(signature []byte, userDataHash [32]byte, l1Height uint64) error {
	publicKey, err := crypto.SigToPub(userDataHash[:], signature)
	if err != nil {
		return fmt.Errorf("failed to convert signature to public key: %w", err)
	}
	addr := crypto.PubkeyToAddress(*publicKey)
	validAddresses := s.batcherAddressesFetcher(l1Height)
	if len(validAddresses) == 0 {
		log.Warn("no valid addresses found", "validAddresses", validAddresses)
		// No valid addresses right now. Need to catch up
		return ErrRetryParsingHotShotPayload
	}
	// if the list of valid addresses doesn't contain the address from the signature, this signature is invalid,
	// and we must return an error.
	if !slices.Contains(validAddresses, addr) {
		log.Warn("batch poster address", "addr", addr, "expected one of", validAddresses)
		return fmt.Errorf("batch poster address does not match")
	}
	return nil
}

func (s *EspressoStreamer) GetCurrentEarliestHotShotBlockNumber() uint64 {
	s.messageLock.RLock()
	defer s.messageLock.RUnlock()
	if msg, exists := s.messageWithMetadataAndPos[s.currentMessagePos]; exists {
		return msg.HotshotHeight
	}
	return s.nextHotshotBlockNum
}

func (s *EspressoStreamer) GetEarliestHotshotBlockForPosition(pos uint64) (uint64, error) {
	s.messageLock.RLock()
	defer s.messageLock.RUnlock()
	if msg, exists := s.messageWithMetadataAndPos[pos]; exists {
		return msg.HotshotHeight, nil
	}
	log.Warn("position is not found in streamer", "pos", pos, "hotshot block", s.nextHotshotBlockNum)
	return 0, fmt.Errorf("earliest hotshot block not found")
}

/* Verify the attestation quote */
func (s *EspressoStreamer) verifyLegacy(attestation []byte, signature [32]byte) error {
	// as of 02/10/2025 there has never been an sgx TEE Caff Node that has signed a transaction meant to be checked by the verify function.
	// Therefore we can hard code espressotee.BatchPoster as we will only ever need to check batch poster pcr0 values
	// to verify the signature on messages.
	_, err := s.espressoSGXVerifier.Verify(nil, attestation, signature)
	if err == nil {
		return nil
	}

	if !strings.Contains(err.Error(), "execution reverted") {
		log.Warn("failed to verify sgx attestation quote", "err", err)
		return ErrRetryParsingHotShotPayload
	}
	return err
}

func (s *EspressoStreamer) parseEspressoTransaction(tx espressoTypes.Bytes, l1Height uint64) ([]*MessageWithMetadataAndPos, error) {
	signature, userDataHash, indices, messages, err := arbutil.ParseHotShotPayload(tx)
	if err != nil {
		log.Warn("failed to parse hotshot payload", "err", err)
		return nil, err
	}
	if len(messages) == 0 {
		return nil, ErrPayloadHadNoMessages
	}
	if len(userDataHash) != 32 {
		log.Warn("user data hash is not 32 bytes")
		return nil, ErrUserDataHashNot32Bytes
	}

	userDataHashArr := [32]byte(userDataHash)

	var success bool
	err = s.verifyBatchPosterSignature(signature, userDataHashArr, l1Height)
	if err == nil {
		success = true
	} else if strings.Contains(err.Error(), ErrRetryParsingHotShotPayload.Error()) {
		log.Warn("retrying to verify batch poster signature", "err", err)
		return nil, err
	} else {
		log.Warn("failed to verify batch poster signature", "err", err)
	}

	if !success && s.espressoSGXVerifier != nil {
		err = s.verifyLegacy(signature, userDataHashArr)
		if err != nil {
			log.Warn("failed to verify attestation quote", "err", err)
			return nil, err
		}
	}

	result := []*MessageWithMetadataAndPos{}

	for i, message := range messages {
		var messageWithMetadata arbostypes.MessageWithMetadata
		err = rlp.DecodeBytes(message, &messageWithMetadata)
		if err != nil {
			log.Warn("failed to decode message", "err", err)
			// Instead of returnning an error, we should just skip this message
			continue
		}
		result = append(result, &MessageWithMetadataAndPos{
			MessageWithMeta: messageWithMetadata,
			Pos:             indices[i],
			HotshotHeight:   s.nextHotshotBlockNum,
		})
		log.Info("Added message to queue", "message", indices[i])
	}
	return result, nil
}

func (s *EspressoStreamer) getEspressoBlockTimestamp(ctx context.Context, blockHeight uint64) (time.Time, error) {
	header, err := s.espressoClient.FetchHeaderByHeight(ctx, blockHeight)
	if err != nil {
		return time.Time{}, fmt.Errorf("unable to fetch header for hotshot block: %w", err)
	}
	seconds, err := safecast.ToInt64(header.Header.GetTimestamp())
	if err != nil {
		return time.Time{}, fmt.Errorf("unable to cast timestamp to int64: %w", err)
	}
	return time.Unix(seconds, 0), nil
}

func (s *EspressoStreamer) RecordTimeDurationBetweenHotshotAndCurrentBlock(nextHotshotBlock uint64, blockProductionTime time.Time) {
	if s.PerfRecorder != nil {
		timestamp, err := s.getEspressoBlockTimestamp(context.Background(), nextHotshotBlock)
		if err != nil {
			log.Warn("unable to fetch header for hotshot block", "err", err)
		} else {
			s.PerfRecorder.SetStartTime(timestamp)
			s.PerfRecorder.SetEndTime(blockProductionTime, fmt.Sprintf("Time duration between hotshot block %d and current block", nextHotshotBlock))
		}
	}
}

func (s *EspressoStreamer) parseDecentralizedTimeboostTransaction(tx espressoTypes.Bytes, l1Height uint64) ([]*MessageWithMetadataAndPos, error) {
	parsedMsgs, err := decentralized_timeboost.ParseTimeboostEspressoTransaction(tx, s.currentMessagePos, s.committeeFetcher)
	if err != nil {
		return nil, err
	}

	var msgs []*MessageWithMetadataAndPos
	if parsedMsgs == nil {
		return msgs, nil
	}

	for _, msg := range parsedMsgs {
		if msg.Pos%100 == 0 {
			log.Info("added timeboost message to queue", "messagePos", msg.Pos, "streamerPos", s.currentMessagePos)
		}
		msgs = append(msgs, &MessageWithMetadataAndPos{
			MessageWithMeta: msg.Message,
			Pos:             msg.Pos,
			HotshotHeight:   s.nextHotshotBlockNum,
		})
	}
	return msgs, nil
}

// Export this function only for testing purpose
func (s *EspressoStreamer) SetSGXVerifier(sgxVerifier espressotee.EspressoSGXVerifierInterface) {
	s.espressoSGXVerifier = sgxVerifier
}

func (s *EspressoStreamer) SetBatcherAddressesFetcher(fetcher func(l1Height uint64) []common.Address) {
	s.batcherAddressesFetcher = fetcher
}

func fetchNextHotshotBlock(
	ctx context.Context,
	espressoClient espressoClient.EspressoClient,
	nextHotshotBlockNum uint64,
	parseHotShotPayloadFn func(tx espressoTypes.Bytes, l1Height uint64) ([]*MessageWithMetadataAndPos, error),
	namespace uint64,
) ([]*MessageWithMetadataAndPos, error) {
	arbTxns, err := espressoClient.FetchTransactionsInBlock(ctx, nextHotshotBlockNum, namespace)
	if err != nil {
		return []*MessageWithMetadataAndPos{}, fmt.Errorf("%w: %w", ErrFailedToFetchTransactions, err)
	}
	if len(arbTxns.Transactions) == 0 {
		return []*MessageWithMetadataAndPos{}, nil
	}

	header, err := espressoClient.FetchHeaderByHeight(ctx, nextHotshotBlockNum)
	l1Height := uint64(0)
	if err != nil {
		return []*MessageWithMetadataAndPos{}, fmt.Errorf("%w: %w", ErrFailedToFetchTransactions, err)
	}

	finalized := header.Header.GetL1Finalized()
	if finalized != nil {
		l1Height = finalized.Number
	}
	result := []*MessageWithMetadataAndPos{}

	for _, tx := range arbTxns.Transactions {
		messages, err := parseHotShotPayloadFn(tx, l1Height)
		if err != nil && !strings.Contains(err.Error(), ErrRetryParsingHotShotPayload.Error()) {
			log.Warn("failed to verify espresso transaction", "err", err)
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, messages...)
	}
	return result, nil
}

func (s *EspressoStreamer) Start(ctxIn context.Context) error {
	s.StopWaiter.Start(ctxIn, s)

	ephemeralErrorHandler := util.NewEphemeralErrorHandler(3*time.Minute, ErrFailedToFetchTransactions.Error(), 1*time.Minute)
	err := s.CallIterativelySafe(func(ctx context.Context) time.Duration {
		if s.nextHotshotBlockNum%100 == 0 {
			log.Info("Now processing hotshot block", "block number", s.nextHotshotBlockNum)
		} else {
			log.Debug("Now processing hotshot block", "block number", s.nextHotshotBlockNum)
		}
		var err error
		if s.isDecentralizedTimeboost {
			err = s.QueueMessagesFromHotshot(ctx, s.parseDecentralizedTimeboostTransaction)
		} else {
			err = s.QueueMessagesFromHotshot(ctx, s.parseEspressoTransaction)
		}

		if err != nil {
			logLevel := log.Error
			logLevel = ephemeralErrorHandler.LogLevel(err, logLevel)
			logLevel("error while queueing messages from hotshot", "err", err)
			return s.retryTime
		} else {
			ephemeralErrorHandler.Reset()
		}
		return 0
	})
	return err
}

func (s *EspressoStreamer) StopAndWait() {
	s.StopWaiter.StopAndWait()
}
