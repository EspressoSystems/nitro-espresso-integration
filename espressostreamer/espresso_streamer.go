package espressostreamer

import (
	"context"
	"errors"
	"fmt"
	"time"

	espressoClient "github.com/EspressoSystems/espresso-sequencer-go/client"
	espressoTypes "github.com/EspressoSystems/espresso-sequencer-go/types"
	"github.com/ccoveille/go-safecast"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/espressotee"
	"github.com/offchainlabs/nitro/util/dbutil"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

const NextHotshotBlockKey = "nextHotshotBlock"

var FailedToFetchTransactionsErr = errors.New("failed to fetch transactions")

type EspressoClientInterface interface {
	FetchLatestBlockHeight(ctx context.Context) (uint64, error)
	FetchTransactionsInBlock(ctx context.Context, blockHeight uint64, namespace uint64) (espressoClient.TransactionsInBlock, error)
	FetchHeaderByHeight(ctx context.Context, blockHeight uint64) (espressoTypes.HeaderImpl, error)
}

type EspressoStreamerInterface interface {
	Next() (*MessageWithMetadataAndPos, error)
	Peek() (*MessageWithMetadataAndPos, error)
	Advance()
	Reset(currentMessagePos uint64, currentHostshotBlock uint64)
	RecordTimeDurationBetweenHotshotAndCurrentBlock(nextHotshotBlock uint64, blockProductionTime time.Time)
	StoreHotshotBlock(db ethdb.Database, nextHotshotBlock uint64) error
	ReadNextHotshotBlockFromDb(db ethdb.Database) (uint64, error)
	GetCurrentEarliestHotShotBlockNumber() uint64
}

type MessageWithMetadataAndPos struct {
	MessageWithMeta arbostypes.MessageWithMetadata
	Pos             uint64
	HotshotHeight   uint64
}

type EspressoStreamer struct {
	stopwaiter.StopWaiter
	espressoClient                EspressoClientInterface
	nextHotshotBlockNum           uint64
	currentMessagePos             uint64
	namespace                     uint64
	retryTime                     time.Duration
	pollingHotshotPollingInterval time.Duration
	messageWithMetadataAndPos     []*MessageWithMetadataAndPos
	espressoTEEVerifier           espressotee.EspressoTEEVerifierInterface

	PerfRecorder    *PerfRecorder
	batchPosterAddr common.Address
}

func NewEspressoStreamer(
	namespace uint64,
	nextHotshotBlockNum uint64,
	retryTime time.Duration,
	pollingHotshotPollingInterval time.Duration,
	espressoTEEVerifier espressotee.EspressoTEEVerifierInterface,
	espressoClientInterface EspressoClientInterface,
	recordPerformance bool,
	batchPosterAddr common.Address,
) *EspressoStreamer {

	var PerfRecorder *PerfRecorder
	if recordPerformance {
		PerfRecorder = NewPerfRecorder()
	}

	return &EspressoStreamer{
		espressoClient:                espressoClientInterface,
		nextHotshotBlockNum:           nextHotshotBlockNum,
		retryTime:                     retryTime,
		pollingHotshotPollingInterval: pollingHotshotPollingInterval,
		namespace:                     namespace,
		espressoTEEVerifier:           espressoTEEVerifier,
		PerfRecorder:                  PerfRecorder,
		batchPosterAddr:               batchPosterAddr,
	}
}

func (s *EspressoStreamer) Reset(currentMessagePos uint64, currentHostshotBlock uint64) {
	s.currentMessagePos = currentMessagePos
	s.nextHotshotBlockNum = currentHostshotBlock
	s.messageWithMetadataAndPos = []*MessageWithMetadataAndPos{}
}

func (s *EspressoStreamer) Next() (*MessageWithMetadataAndPos, error) {
	result, err := s.Peek()
	if err != nil {
		return nil, err
	}

	// Advance the current message position, so that the next call to
	// `Peek` or `Next` will return the next message
	s.Advance()
	return result, nil
}

func (s *EspressoStreamer) Peek() (*MessageWithMetadataAndPos, error) {
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
		return s.messageWithMetadataAndPos[messageIndex], nil
	}

	condition := func(messages []*MessageWithMetadataAndPos) bool {
		for _, message := range messages {
			if message.Pos == s.currentMessagePos {
				return true
			}
		}
		return false
	}

	err := s.QueueMessagesFromHotShotUntil(context.Background(), s.parseEspressoTransaction, condition)
	if err != nil {
		return nil, err
	}

	messageIndex = FilterAndFind(&s.messageWithMetadataAndPos, compareMessageWithCurrentPos)

	if messageIndex >= 0 {
		return s.messageWithMetadataAndPos[messageIndex], nil
	}
	return nil, fmt.Errorf("message not found, please check the condition")
}

// Call this function to advance the streamer to the next message
func (s *EspressoStreamer) Advance() {
	s.currentMessagePos += 1
}

// This function keep fetching hotshot blocks and parsing them until the condition is met.
// It is a do-while loop, which means it will always execute at least once.
//
// Expose the *parseHotShotPayloadFn* to the caller for testing purposes
func (s *EspressoStreamer) QueueMessagesFromHotShotUntil(
	ctx context.Context,
	parseHotShotPayloadFn func(tx espressoTypes.Bytes) ([]*MessageWithMetadataAndPos, error),
	condition func(messages []*MessageWithMetadataAndPos) bool,
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			messages, err := fetchNextHotshotBlock(ctx, s.espressoClient, s.nextHotshotBlockNum, parseHotShotPayloadFn, s.namespace)
			if err != nil {
				continue
			}

			if len(messages) > 0 {
				s.messageWithMetadataAndPos = append(s.messageWithMetadataAndPos, messages...)
			}
			s.nextHotshotBlockNum += 1

			if condition(messages) {
				return nil
			}

			time.Sleep(s.pollingHotshotPollingInterval)
		}
	}
}

func (s *EspressoStreamer) verifyBatchPosterSignature(signature []byte, userDataHash [32]byte) error {
	publicKey, err := crypto.SigToPub(userDataHash[:], signature)
	if err != nil {
		return fmt.Errorf("failed to convert signature to public key: %w", err)
	}
	addr := crypto.PubkeyToAddress(*publicKey)
	if addr != s.batchPosterAddr {
		log.Warn("batch poster address", "addr", addr, "expected", s.batchPosterAddr)
		return fmt.Errorf("batch poster address does not match")
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
func (s *EspressoStreamer) verifySignature(attestation []byte, signature [32]byte) error {

	_, err := s.espressoTEEVerifier.Verify(&bind.CallOpts{}, attestation, signature)
	if err != nil {
		return fmt.Errorf("call to the espressoTEEVerifier contract failed: %w", err)
	}
	return nil
}

func (s *EspressoStreamer) parseEspressoTransaction(tx espressoTypes.Bytes) ([]*MessageWithMetadataAndPos, error) {
	signature, userDataHash, indices, messages, err := arbutil.ParseHotShotPayload(tx)
	if err != nil {
		log.Warn("failed to parse hotshot payload", "err", err)
		return nil, err
	}
	// if attestation verification fails, we should skip this transaction
	// Parse the messages
	if len(userDataHash) != 32 {
		log.Warn("user data hash is not 32 bytes")
		return nil, fmt.Errorf("user data hash is not 32 bytes")
	}

	userDataHashArr := [32]byte(userDataHash)

	var success bool
	err = s.verifyBatchPosterSignature(signature, userDataHashArr)
	if err == nil {
		success = true
	} else {
		log.Warn("failed to verify batch poster signature", "err", err)
	}

	if !success {
		err = s.verifySignature(signature, userDataHashArr)
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

func (s *EspressoStreamer) ReadNextHotshotBlockFromDb(db ethdb.Database) (uint64, error) {
	var nextHotshotBlock uint64
	nextHotshotBytes, err := db.Get([]byte(NextHotshotBlockKey))
	if err != nil && !dbutil.IsErrNotFound(err) {
		return 0, fmt.Errorf("failed to get next hotshot block: %w", err)
	}
	if nextHotshotBytes != nil {
		err = rlp.DecodeBytes(nextHotshotBytes, &nextHotshotBlock)
		if err != nil {
			return 0, fmt.Errorf("failed to decode next hotshot block: %w", err)
		}
	}

	return nextHotshotBlock, nil
}

func (s *EspressoStreamer) StoreHotshotBlock(db ethdb.Database, nextHotshotBlock uint64) error {
	nextHotshotBytes, err := rlp.EncodeToBytes(nextHotshotBlock)
	if err != nil {
		return fmt.Errorf("failed to encode next hotshot block: %w", err)
	}

	err = db.Put([]byte(NextHotshotBlockKey), nextHotshotBytes)
	if err != nil {
		return fmt.Errorf("failed to put next hotshot block: %w", err)
	}

	return nil
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

func fetchNextHotshotBlock(
	ctx context.Context,
	espressoClient EspressoClientInterface,
	nextHotshotBlockNum uint64,
	parseHotShotPayloadFn func(tx espressoTypes.Bytes) ([]*MessageWithMetadataAndPos, error),
	namespace uint64,
) ([]*MessageWithMetadataAndPos, error) {
	arbTxns, err := espressoClient.FetchTransactionsInBlock(ctx, nextHotshotBlockNum, namespace)
	if err != nil {
		return []*MessageWithMetadataAndPos{}, fmt.Errorf("%w: %w", FailedToFetchTransactionsErr, err)
	}

	result := []*MessageWithMetadataAndPos{}

	for _, tx := range arbTxns.Transactions {
		messages, err := parseHotShotPayloadFn(tx)
		if err != nil {
			log.Warn("failed to verify espresso transaction", "err", err)
			continue
		}
		result = append(result, messages...)
	}
	return result, nil
}
