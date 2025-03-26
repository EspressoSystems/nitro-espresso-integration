package arbnode

import (
	"context"
	"fmt"
	"math/big"
	"time"

	espressoClient "github.com/EspressoSystems/espresso-sequencer-go/client"
	flag "github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/espressostreamer"
	"github.com/offchainlabs/nitro/execution/gethexec"
	"github.com/offchainlabs/nitro/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/util/dbutil"
	"github.com/offchainlabs/nitro/util/headerreader"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

var (
	DelayedFetcherCurrentL1BlockKey = []byte("delayedFetcherCurrentL1Block")
	// To not to mess with the existing schema, we use another prefix
	DelayedMessagePrefix = []byte("x")
)

type DelayedMessageFetcherInterface interface {
	getDelayedMessage(index uint64) (*arbostypes.L1IncomingMessage, error)
	reset(seqNum uint64)
}

type DelayedMessageFetcher struct {
	fromBlock     uint64
	delayedBridge *DelayedBridge
	l1Reader      *ethclient.Client

	db ethdb.Database
}

func NewDelayedMessageFetcher(delayedBridge *DelayedBridge, l1Reader *ethclient.Client, db ethdb.Database) *DelayedMessageFetcher {
	var fromBlock uint64
	fromBlock, err := readCurrentL1BlockFromDb(db)
	if err != nil {
		log.Crit("failed to read l1 block from db", "err", err)
		return nil
	}

	if fromBlock == 0 {
		fromBlock = delayedBridge.fromBlock
	}

	return &DelayedMessageFetcher{
		fromBlock:     fromBlock,
		delayedBridge: delayedBridge,
		l1Reader:      l1Reader,
		db:            db,
	}
}

func (f *DelayedMessageFetcher) getDelayedMessage(index uint64) (*arbostypes.L1IncomingMessage, error) {
	msg, err := f.readDelayedMessage(index)
	if err != nil && !dbutil.IsErrNotFound(err) {
		return nil, err
	}
	if msg != nil && f.fromBlock >= msg.ParentChainBlockNumber {
		return msg.Message, nil
	}

	currL1, err := f.l1Reader.BlockNumber(context.Background())
	if err != nil {
		return nil, err
	}
	if currL1 < f.fromBlock {
		return nil, fmt.Errorf("l1 block number %d is less than from block %d", currL1, f.fromBlock)
	}

	startBlock := f.fromBlock
	endBlock := currL1
	hasFound := false

	// This value is from the InboxReader default blocks-to-read.
	// TODO: Make this configurable.
	blocksToRead := uint64(100)

	batch := f.db.NewBatch()
	for startBlock <= endBlock && !hasFound {
		from := big.NewInt(0).SetUint64(startBlock)
		to := big.NewInt(0).SetUint64(startBlock + blocksToRead)
		msgs, err := f.delayedBridge.LookupMessagesInRange(context.Background(), from, to, nil)
		if err != nil {
			return nil, err
		}
		for _, msg := range msgs {
			seqNum, err := msg.Message.Header.SeqNum()
			if err != nil {
				return nil, err
			}
			if seqNum == index {
				hasFound = true
			}
			err = f.storeDelayedMessage(batch, seqNum, *msg)
			if err != nil {
				return nil, err
			}
		}
		startBlock = startBlock + blocksToRead + 1
	}

	if startBlock <= endBlock {
		f.fromBlock = startBlock
	} else {
		f.fromBlock = endBlock + 1
	}

	err = storeCurrentL1Block(batch, f.fromBlock)
	if err != nil {
		return nil, err
	}

	err = batch.Write()
	if err != nil {
		return nil, err
	}

	if !hasFound {
		return nil, fmt.Errorf("no message found for pos %d", index)
	}

	result, err := f.readDelayedMessage(index)
	if err != nil {
		return nil, err
	}
	return result.Message, nil
}

func (f *DelayedMessageFetcher) reset(seqNum uint64) {
	msg, err := f.readDelayedMessage(seqNum)
	if err != nil {
		log.Crit("failed to read delayed message", "err", err)
		return
	}
	f.fromBlock = msg.ParentChainBlockNumber + 1
}

func (f *DelayedMessageFetcher) storeDelayedMessage(batch ethdb.Batch, seqNum uint64, msg DelayedInboxMessage) error {
	key := dbKey(DelayedMessagePrefix, seqNum)
	encodedMsg, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return fmt.Errorf("failed to encode delayed message: %w", err)
	}
	return batch.Put(key, encodedMsg)
}

func (f *DelayedMessageFetcher) readDelayedMessage(seqNum uint64) (*DelayedInboxMessage, error) {
	key := dbKey(DelayedMessagePrefix, seqNum)
	encodedMsg, err := f.db.Get(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get delayed message: %w", err)
	}
	var msg DelayedInboxMessage
	err = rlp.DecodeBytes(encodedMsg, &msg)
	if err != nil {
		return nil, fmt.Errorf("failed to decode delayed message: %w", err)
	}
	return &msg, nil
}

type EspressoCaffNodeConfig struct {
	Enable                  bool          `koanf:"enable"`
	HotShotUrls             []string      `koanf:"hotshot-urls"`
	NextHotshotBlock        uint64        `koanf:"next-hotshot-block"`
	Namespace               uint64        `koanf:"namespace"`
	RetryTime               time.Duration `koanf:"retry-time"`
	HotshotPollingInterval  time.Duration `koanf:"hotshot-polling-interval"`
	EspressoTEEVerifierAddr string        `koanf:"espresso-tee-verifier-addr"`
	BatchPosterAddr         string        `koanf:"batch-poster-addr"`
	RecordPerformance       bool          `koanf:"record-performance"`
}

var DefaultEspressoCaffNodeConfig = EspressoCaffNodeConfig{
	Enable:                  false,
	HotShotUrls:             []string{},
	NextHotshotBlock:        1,
	Namespace:               0,
	RetryTime:               time.Second * 2,
	HotshotPollingInterval:  time.Millisecond * 100,
	EspressoTEEVerifierAddr: "",
	BatchPosterAddr:         "",
	RecordPerformance:       false,
}

func EspressoCaffNodeConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Bool(prefix+".enable", DefaultEspressoCaffNodeConfig.Enable, "enable espresso caff node")
	f.StringSlice(prefix+".hotshot-urls", DefaultEspressoCaffNodeConfig.HotShotUrls, "hotshot urls")
	f.Uint64(prefix+".next-hotshot-block", DefaultEspressoCaffNodeConfig.NextHotshotBlock, "the hotshot block number from which the caff node will read")
	f.Uint64(prefix+".namespace", DefaultEspressoCaffNodeConfig.Namespace, "the namespace of the chain in Espresso Network, usually the chain id")
	f.Duration(prefix+".retry-time", DefaultEspressoCaffNodeConfig.RetryTime, "retry time after a failure")
	f.Duration(prefix+".hotshot-polling-interval", DefaultEspressoCaffNodeConfig.HotshotPollingInterval, "time after a success")
	f.String(prefix+".espresso-tee-verifier-addr", "", "tee verifier address")
	f.String(prefix+".batch-poster-addr", DefaultEspressoCaffNodeConfig.BatchPosterAddr, "batch poster address that is used to verify the signature of the hotshot transactions")
	f.Bool(prefix+".record-performance", DefaultEspressoCaffNodeConfig.RecordPerformance, "record performance of the caff node")
}

type EspressoCaffNodeConfigFetcher func() *EspressoCaffNodeConfig

type EspressoCaffNode struct {
	stopwaiter.StopWaiter

	executionEngine  *gethexec.ExecutionEngine
	espressoStreamer espressostreamer.EspressoStreamerInterface

	configFetcher EspressoCaffNodeConfigFetcher
	delayedCount  uint64
	db            ethdb.Database

	delayedMessageFetcher DelayedMessageFetcherInterface
}

func NewEspressoCaffNode(
	configFetcher EspressoCaffNodeConfigFetcher,
	execEngine *gethexec.ExecutionEngine,
	delayedBridge *DelayedBridge,
	l1Reader *headerreader.HeaderReader,
	db ethdb.Database,
	recordPerformance bool,
) *EspressoCaffNode {
	if !configFetcher().Enable {
		return nil
	}

	if l1Reader == nil {
		log.Crit("l1Reader is nil")
		return nil
	}

	espressoTEEVerifierCaller, err := bridgegen.NewEspressoTEEVerifier(
		common.HexToAddress(configFetcher().EspressoTEEVerifierAddr),
		l1Reader.Client())

	if err != nil || espressoTEEVerifierCaller == nil {
		log.Crit("failed to create espressoTEEVerifierCaller", "err", err)
		return nil
	}

	espressoStreamer := espressostreamer.NewEspressoStreamer(configFetcher().Namespace,
		configFetcher().NextHotshotBlock,
		configFetcher().RetryTime,
		configFetcher().HotshotPollingInterval,
		espressoTEEVerifierCaller,
		espressoClient.NewMultipleNodesClient(configFetcher().HotShotUrls),
		recordPerformance,
		common.HexToAddress(configFetcher().BatchPosterAddr),
	)

	delayedMessageFetcher := NewDelayedMessageFetcher(delayedBridge, l1Reader.Client(), db)

	return &EspressoCaffNode{
		configFetcher:         configFetcher,
		executionEngine:       execEngine,
		delayedMessageFetcher: delayedMessageFetcher,
		espressoStreamer:      espressoStreamer,
		delayedCount:          1,
		db:                    db,
	}
}

func (n *EspressoCaffNode) nextMessage() (*espressostreamer.MessageWithMetadataAndPos, error) {
	messageWithMetadataAndPos, err := n.espressoStreamer.Next()
	if err != nil {
		return nil, err
	}

	if messageWithMetadataAndPos == nil {
		return nil, nil
	}

	if messageWithMetadataAndPos.MessageWithMeta.DelayedMessagesRead == n.delayedCount+1 {
		// If this is delayed message, we need to get the message from L1
		// and replace the message in the messageWithMetadataAndPos
		message, err := n.delayedMessageFetcher.getDelayedMessage(n.delayedCount)
		if err != nil {
			n.espressoStreamer.Reset(messageWithMetadataAndPos.Pos, messageWithMetadataAndPos.HotshotHeight)
			return nil, err
		}
		n.delayedCount++
		messageWithMetadataAndPos.MessageWithMeta.Message = message
	}
	return messageWithMetadataAndPos, nil
}

func (n *EspressoCaffNode) createBlock() (returnValue bool) {

	lastBlockHeader := n.executionEngine.Bc().CurrentBlock()

	messageWithMetadataAndPos, err := n.nextMessage()
	if err != nil {
		log.Warn("unable to get next message", "err", err)
		return false
	}

	if messageWithMetadataAndPos == nil {
		log.Debug("no message found. Should not happen")
		return false
	}

	messageWithMetadata := messageWithMetadataAndPos.MessageWithMeta

	// Get the state of the database at the last block
	statedb, err := n.executionEngine.Bc().StateAt(lastBlockHeader.Root)
	if err != nil {
		log.Error("failed to get state at last block header", "err", err)
		log.Debug("Resetting espresso streamer", "currentMessagePos",
			messageWithMetadataAndPos.Pos, "currentHostshotBlock",
			messageWithMetadataAndPos.HotshotHeight)
		n.espressoStreamer.Reset(messageWithMetadataAndPos.Pos, messageWithMetadataAndPos.HotshotHeight)
		return false
	}

	log.Info("Initial State", "lastBlockHash", lastBlockHeader.Hash(), "lastBlockStateRoot", lastBlockHeader.Root)
	startTime := time.Now()

	// Run the Produce block function in replay mode
	// This is the core function that is used by replay.wasm to validate the block
	block, receipts, err := arbos.ProduceBlock(messageWithMetadata.Message,
		messageWithMetadata.DelayedMessagesRead,
		lastBlockHeader,
		statedb,
		n.executionEngine.Bc(),
		n.executionEngine.Bc().Config(),
		false,
		core.MessageReplayMode)

	if err != nil || block == nil {
		log.Error("Failed to produce block", "err", err)
		log.Debug("Resetting espresso streamer", "currentMessagePos",
			messageWithMetadataAndPos.Pos, "currentHostshotBlock",
			messageWithMetadataAndPos.HotshotHeight)
		n.espressoStreamer.Reset(messageWithMetadataAndPos.Pos, messageWithMetadataAndPos.HotshotHeight)
		return false
	}

	blockCalcTime := time.Since(startTime)

	log.Info("Produced block", "block", block.Hash(), "blockNumber", block.Number(), "receipts", len(receipts))

	err = n.executionEngine.AppendBlock(block, statedb, receipts, blockCalcTime)
	if err != nil {
		log.Error("Failed to append block", "err", err)
		log.Debug("Resetting espresso streamer", "currentMessagePos",
			messageWithMetadataAndPos.Pos, "currentHostshotBlock",
			messageWithMetadataAndPos.HotshotHeight)
		n.espressoStreamer.Reset(messageWithMetadataAndPos.Pos, messageWithMetadataAndPos.HotshotHeight)
		return false
	}

	n.espressoStreamer.RecordTimeDurationBetweenHotshotAndCurrentBlock(messageWithMetadataAndPos.HotshotHeight, time.Now())

	err = n.espressoStreamer.StoreHotshotBlock(n.db, messageWithMetadataAndPos.HotshotHeight)
	if err != nil {
		log.Error("Failed to store hotshot block", "err", err)
		log.Debug("Resetting espresso streamer", "currentMessagePos",
			messageWithMetadataAndPos.Pos, "currentHostshotBlock",
			messageWithMetadataAndPos.HotshotHeight)
		n.espressoStreamer.Reset(messageWithMetadataAndPos.Pos, messageWithMetadataAndPos.HotshotHeight)
		return false
	}

	return true
}

func (n *EspressoCaffNode) Start(ctx context.Context) error {
	n.StopWaiter.Start(ctx, n)
	err := n.espressoStreamer.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start espresso streamer: %w", err)
	}
	// This is +1 because the current block is the block after the last processed block
	currentBlockNum := n.executionEngine.Bc().CurrentBlock().Number.Uint64() + 1
	currentMessagePos, err := n.executionEngine.BlockNumberToMessageIndex(currentBlockNum)
	if err != nil {
		return fmt.Errorf("failed to convert block number to message index: %w", err)
	}
	nextHotshotBlock, err := n.espressoStreamer.ReadNextHotshotBlockFromDb(n.db)
	if err != nil {
		log.Crit("failed to read  next hotshot block", "err", err)
		return nil
	}

	if nextHotshotBlock == 0 {
		// No next hotshot block found, so we need to start from config.CaffNodeConfig.NextHotshotBlock
		nextHotshotBlock = n.configFetcher().NextHotshotBlock
		if nextHotshotBlock == 0 {
			log.Crit("No next hotshot block found in database, and no config.CaffNodeConfig.NextHotshotBlock set")
		}
	}
	// The reason we do the reset here is because database is only initialized after Caff node is initialized
	// so if we want to read the current position from the database, we need to reset the streamer
	// during the start of the espresso streamer and caff node
	log.Debug("Starting streamer at", "nextHotshotBlock", nextHotshotBlock, "currentMessagePos", currentMessagePos)
	n.espressoStreamer.Reset(uint64(currentMessagePos), nextHotshotBlock)

	err = n.CallIterativelySafe(func(ctx context.Context) time.Duration {
		madeBlock := n.createBlock()
		if madeBlock {
			return n.configFetcher().HotshotPollingInterval
		}
		return n.configFetcher().RetryTime
	})
	if err != nil {
		return fmt.Errorf("failed to start node, error in createBlock: %w", err)
	}

	return nil
}

func storeCurrentL1Block(batch ethdb.Batch, fromBlock uint64) error {
	blockNumberBytes, err := rlp.EncodeToBytes(fromBlock)
	if err != nil {
		return fmt.Errorf("failed to encode next hotshot block: %w", err)
	}

	err = batch.Put([]byte(DelayedFetcherCurrentL1BlockKey), blockNumberBytes)
	if err != nil {
		return fmt.Errorf("failed to put next hotshot block: %w", err)
	}

	return nil
}

func readCurrentL1BlockFromDb(db ethdb.Database) (uint64, error) {
	var blockNumber uint64
	blockNumberBytes, err := db.Get([]byte(DelayedFetcherCurrentL1BlockKey))
	if err != nil && !dbutil.IsErrNotFound(err) {
		return 0, fmt.Errorf("failed to get next hotshot block: %w", err)
	}
	if blockNumberBytes != nil {
		err = rlp.DecodeBytes(blockNumberBytes, &blockNumber)
		if err != nil {
			return 0, fmt.Errorf("failed to decode next hotshot block: %w", err)
		}
	}

	return blockNumber, nil
}
