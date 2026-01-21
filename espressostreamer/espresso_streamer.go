package espressostreamer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	espressoClient "github.com/EspressoSystems/espresso-network/sdks/go/client"
	espressoTypes "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/ccoveille/go-safecast"
	"github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/espressotee"
	"github.com/offchainlabs/nitro/util"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

const HOTSHOT_RANGE_LIMIT = 100

var (
	ErrFailedToFetchTransactions  = errors.New("failed to fetch transactions")
	ErrPayloadHadNoMessages       = errors.New("ParseHotShotPayload found no messages, the transaction may be empty")
	ErrUserDataHashNot32Bytes     = errors.New("user data hash is not 32 bytes")
	ErrRetryParsingHotShotPayload = errors.New("failed to parse hotshot payload, but need retry")
)

type EspressoStreamerInterface interface {
	Start(ctx context.Context) error
	Next(ctx context.Context) *MessageWithMetadataAndPos
	// Peek returns the next message in the streamer's buffer. If the message is not
	// in the buffer, it will return nil.
	Peek(ctx context.Context) *MessageWithMetadataAndPos
	// Advance moves the current message position to the next message.
	Advance()
	// Reset sets the current message position and the next hotshot block number.
	Reset(currentMessagePos uint64, currentHostshotBlock uint64)
	// RecordTimeDurationBetweenHotshotAndCurrentBlock records the time duration between
	// the next hotshot block and the current block.
	RecordTimeDurationBetweenHotshotAndCurrentBlock(nextHotshotBlock uint64, blockProductionTime time.Time)
	GetCurrentEarliestHotShotBlockNumber() uint64

	SetBatcherAddressesFetcher(fetcher func(l1Height uint64, address common.Address) (bool, error))
	CanBatcherAddressSend(ctx context.Context, address common.Address) (bool, error)
	StopAndWait()
}

type MessageWithMetadataAndPos struct {
	MessageWithMeta arbostypes.MessageWithMetadata
	Pos             uint64
	HotshotHeight   uint64
}

type DangerousEspressoStreamerConfig struct {
	MinimumHotshotBlockNum uint64 `koanf:"minimum-hotshot-block-num"`
}

var DefaultDangerousEspressoStreamerConfig = DangerousEspressoStreamerConfig{
	MinimumHotshotBlockNum: 0,
}

func DangerousEspressoStreamerConfigAddOptions(prefix string, f *pflag.FlagSet) {
	f.Uint64(prefix+".minimum-hotshot-block-num", DefaultDangerousEspressoStreamerConfig.MinimumHotshotBlockNum, "minimum hotshot block number")
}

type EspressoStreamerConfig struct {
	HotShotBlock          uint64                          `koanf:"hotshot-block"`
	TxnsPollingInterval   time.Duration                   `koanf:"txns-polling-interval"`
	AddressMonitorStartL1 uint64                          `koanf:"address-monitor-start-l1"`
	AddressMonitorStep    uint64                          `koanf:"address-monitor-step"`
	Dangerous             DangerousEspressoStreamerConfig `koanf:"dangerous"`
}

var DefaultEspressoStreamerConfig = EspressoStreamerConfig{
	HotShotBlock: 1,
	// By default, no minimum hotshot block number is enforced
	Dangerous: DefaultDangerousEspressoStreamerConfig,
	// Hotshot currently produces blocks at average of 2 seconds
	// We set it to 1 second to get updates more often than blocks are produced
	TxnsPollingInterval:   time.Second,
	AddressMonitorStartL1: 1,
	AddressMonitorStep:    100,
}

func EspressoStreamerConfigAddOptions(prefix string, f *pflag.FlagSet) {
	f.Uint64(prefix+".hotshot-block", DefaultEspressoStreamerConfig.HotShotBlock, "specifies the hotshot block number to start the espresso streamer on")
	f.Uint64(prefix+".address-monitor-step", DefaultEspressoStreamerConfig.AddressMonitorStep, "specifies the number of blocks at a time to query when searching for logs emitted for updating valid batcher addresses.")
	f.Uint64(prefix+".address-monitor-start-l1", DefaultEspressoStreamerConfig.AddressMonitorStartL1, "specifies the l1 block number when this rollup started posting to monitor addresses")
	f.Duration(prefix+".txns-polling-interval", DefaultEspressoStreamerConfig.TxnsPollingInterval, "interval between polling for transactions to be included in the block")

	DangerousEspressoStreamerConfigAddOptions(prefix+".dangerous", f)
}

type EspressoStreamer struct {
	stopwaiter.StopWaiter
	espressoClient            espressoClient.EspressoClient
	nextHotshotBlockNum       uint64
	currentMessagePos         uint64
	namespace                 uint64
	messageWithMetadataAndPos []*MessageWithMetadataAndPos
	espressoSGXVerifier       espressotee.EspressoSGXVerifierInterface

	messageLock sync.Mutex
	retryTime   time.Duration

	PerfRecorder *PerfRecorder

	batcherAddressesFetcher         func(l1Height uint64, address common.Address) (bool, error)
	dangerousMinimumHotshotBlockNum uint64
}

var _ EspressoStreamerInterface = (*EspressoStreamer)(nil)

func NewEspressoStreamer(
	namespace uint64,
	nextHotshotBlockNum uint64,
	espressoSGXVerifier espressotee.EspressoSGXVerifierInterface,
	espressoClient espressoClient.EspressoClient,
	recordPerformance bool,
	batcherAddressesFetcher func(l1Height uint64, address common.Address) (bool, error),
	retryTime time.Duration,
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
		dangerousMinimumHotshotBlockNum: dangerousMinimumHotshotBlockNum,
	}
}

func (s *EspressoStreamer) CanBatcherAddressSend(ctx context.Context, address common.Address) (bool, error) {
	if s.batcherAddressesFetcher == nil {
		return false, errors.New("batcher addresses fetcher not set")
	}
	latest, err := s.espressoClient.FetchLatestBlockHeight(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to fetch espresso latest block height: %w", err)
	}
	// Even though we can query the latest block height, the node may not yet serve
	// the header at that exact height. Using `latest-1` avoids spurious errors
	// where this function would otherwise always fail. This is safe because
	// Espresso block production is much faster than L1, and the L1 lag has
	// already been accounted for in the batcher address monitor.
	// TODO: Figure out why this doesn't work without `-1`.
	// It might be just a dev node issue.
	header, err := s.espressoClient.FetchHeaderByHeight(ctx, latest-1)
	if err != nil {
		return false, fmt.Errorf("failed to fetch espresso block header: %w", err)
	}
	l1Finalized := header.Header.GetL1Finalized()
	if l1Finalized == nil {
		return false, fmt.Errorf("l1 finalized not found")
	}
	return s.batcherAddressesFetcher(l1Finalized.Number, address)
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
	return s.currentMessagePos + CountUniqueEntries(&s.messageWithMetadataAndPos)
}

func (s *EspressoStreamer) Reset(currentMessagePos uint64, currentHostshotBlock uint64) {
	s.messageLock.Lock()
	defer s.messageLock.Unlock()

	s.currentMessagePos = currentMessagePos
	s.nextHotshotBlockNum = currentHostshotBlock
	if currentHostshotBlock < s.dangerousMinimumHotshotBlockNum {
		s.nextHotshotBlockNum = s.dangerousMinimumHotshotBlockNum
	}

	s.messageWithMetadataAndPos = []*MessageWithMetadataAndPos{}
}

func (s *EspressoStreamer) Next(ctx context.Context) *MessageWithMetadataAndPos {
	result := s.Peek(ctx)
	if result == nil {
		return nil
	}

	// Advance the current message position, so that the next call to
	// `Peek` or `Next` will return the next message
	s.Advance()
	return result
}

func (s *EspressoStreamer) Peek(ctx context.Context) *MessageWithMetadataAndPos {
	s.messageLock.Lock()
	defer s.messageLock.Unlock()

	compareMessageWithCurrentPos := func(msg *MessageWithMetadataAndPos) int {
		if msg.Pos == s.currentMessagePos {
			return FilterAndFind_Target
		}
		if msg.Pos < s.currentMessagePos {
			return FilterAndFind_Remove
		}
		return FilterAndFind_Keep
	}

	messageIndex := FilterAndFind(&s.messageWithMetadataAndPos, compareMessageWithCurrentPos)

	if messageIndex >= 0 {
		return s.messageWithMetadataAndPos[messageIndex]
	}

	return nil
}

// Call this function to advance the streamer to the next message
func (s *EspressoStreamer) Advance() {
	s.currentMessagePos += 1
}

// This function keep fetching hotshot blocks and parsing them until the condition is met.
// It is a do-while loop, which means it will always execute at least once.
//
// Expose the *parseHotShotPayloadFn* to the caller for testing purposes
func (s *EspressoStreamer) QueueMessagesFromHotshot(
	ctx context.Context,
	parseHotShotPayloadFn func(tx espressoTypes.Bytes, l1Height uint64) ([]*MessageWithMetadataAndPos, error),
) error {
	s.messageLock.Lock()
	defer s.messageLock.Unlock()

	messages, toBlock, err := fetchNextHotshotBlock(
		ctx,
		s.espressoClient,
		s.nextHotshotBlockNum,
		parseHotShotPayloadFn,
		s.namespace,
	)
	if err != nil {
		return err
	}

	if len(messages) > 0 {
		s.messageWithMetadataAndPos = append(s.messageWithMetadataAndPos, messages...)
	}
	s.nextHotshotBlockNum = toBlock
	return nil
}

func (s *EspressoStreamer) verifyBatchPosterSignature(signature []byte, userDataHash [32]byte, l1Height uint64) error {
	publicKey, err := crypto.SigToPub(userDataHash[:], signature)
	if err != nil {
		return fmt.Errorf("failed to convert signature to public key: %w", err)
	}
	addr := crypto.PubkeyToAddress(*publicKey)
	valid, err := s.batcherAddressesFetcher(l1Height, addr)
	if err != nil {
		log.Warn("failed to get valid addresses", "err", err)
		return ErrRetryParsingHotShotPayload
	}
	if !valid {
		log.Error("address not valid", "addr", addr)
		// Address not valid. Need to catch up
		return fmt.Errorf("address not valid: %v", addr)
	}
	return nil
}

func (s *EspressoStreamer) GetCurrentEarliestHotShotBlockNumber() uint64 {
	if len(s.messageWithMetadataAndPos) == 0 {
		// This case means that the espresso streamer is empty and the earliest hotshot block number
		// is the next hotshot block number.
		return s.nextHotshotBlockNum
	}
	return s.messageWithMetadataAndPos[0].HotshotHeight
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
		if indices[i] < s.currentMessagePos {
			log.Warn("message index is less than current message pos, skipping", "messageIndex", indices[i], "currentMessagePos", s.currentMessagePos)
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

// Export this function only for testing purpose
func (s *EspressoStreamer) SetSGXVerifier(sgxVerifier espressotee.EspressoSGXVerifierInterface) {
	s.espressoSGXVerifier = sgxVerifier
}

func (s *EspressoStreamer) SetBatcherAddressesFetcher(fetcher func(l1Height uint64, address common.Address) (bool, error)) {
	s.batcherAddressesFetcher = fetcher
}

func fetchNextHotshotBlock(
	ctx context.Context,
	espressoClient espressoClient.EspressoClient,
	nextHotshotBlockNum uint64,
	parseHotShotPayloadFn func(tx espressoTypes.Bytes, l1Height uint64) ([]*MessageWithMetadataAndPos, error),
	namespace uint64,
) ([]*MessageWithMetadataAndPos, uint64, error) {

	// get the current hotshot block
	latestBlockHeight, err := espressoClient.FetchLatestBlockHeight(ctx)
	if err != nil {
		return []*MessageWithMetadataAndPos{}, 0, fmt.Errorf("%w: %w", ErrFailedToFetchTransactions, err)
	}

	fromBlock := nextHotshotBlockNum
	toBlock := latestBlockHeight

	if latestBlockHeight-nextHotshotBlockNum > HOTSHOT_RANGE_LIMIT {
		toBlock = nextHotshotBlockNum + HOTSHOT_RANGE_LIMIT
	}

	// this means we have no blocks to process and we are all caught up
	if fromBlock == toBlock {
		return []*MessageWithMetadataAndPos{}, toBlock, nil
	}

	// here we are fetching transactions in range [fromBlock, toBlock) exclusive
	//  by default FetchNamespaceTransactionsInRange is exclusive of the last element
	namespaceTransactionRangeData, err := espressoClient.FetchNamespaceTransactionsInRange(ctx, fromBlock, toBlock, namespace)
	if err != nil {
		return []*MessageWithMetadataAndPos{}, 0, fmt.Errorf("%w: %w", ErrFailedToFetchTransactions, err)
	}
	if len(namespaceTransactionRangeData) == 0 {
		// no transactions found in this range is a valid state (e.g., empty blocks), not an error
		return []*MessageWithMetadataAndPos{}, toBlock, nil
	}

	// we are subtracting 1 here because FetchNamespaceTransactionsInRange is exclusive of the last element
	header, err := espressoClient.FetchHeaderByHeight(ctx, toBlock-1)
	l1Height := uint64(0)
	if err != nil {
		return []*MessageWithMetadataAndPos{}, 0, fmt.Errorf("%w: %w", ErrFailedToFetchTransactions, err)
	}

	finalized := header.Header.GetL1Finalized()
	if finalized != nil {
		l1Height = finalized.Number
	}
	result := []*MessageWithMetadataAndPos{}

	for _, namespaceTransactionData := range namespaceTransactionRangeData {
		for _, tx := range namespaceTransactionData.Transactions {
			txPayloadBytes := tx.Payload
			messages, err := parseHotShotPayloadFn(txPayloadBytes, l1Height)
			if err != nil && !strings.Contains(err.Error(), ErrRetryParsingHotShotPayload.Error()) {
				log.Warn("failed to verify espresso transaction", "err", err)
				continue
			}
			if err != nil {
				return nil, 0, err
			}
			result = append(result, messages...)
		}
	}

	return result, toBlock, nil
}

func (s *EspressoStreamer) Start(ctxIn context.Context) error {
	s.StopWaiter.Start(ctxIn, s)

	ephemeralErrorHandler := util.NewEphemeralErrorHandler(3*time.Minute, ErrFailedToFetchTransactions.Error(), 1*time.Minute)
	err := s.CallIterativelySafe(func(ctx context.Context) time.Duration {
		if s.nextHotshotBlockNum%1000 == 0 {
			log.Info("Now processing hotshot block", "block number", s.nextHotshotBlockNum)
		} else {
			log.Debug("Now processing hotshot block", "block number", s.nextHotshotBlockNum)
		}
		err := s.QueueMessagesFromHotshot(ctx, s.parseEspressoTransaction)
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
