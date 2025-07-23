package arbnode

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/offchainlabs/nitro/espressostreamer"
	"github.com/offchainlabs/nitro/util/dbutil"
	"github.com/offchainlabs/nitro/util/headerreader"
)

var (
	DelayedFetcherCurrentFromBlockKey = []byte("espressoDelayedFetcherCurrentFromBlock")
	DelayedMessageCountKey            = []byte("espressoDelayedMessageCount")
	// To not to mess with the existing schema, we use another prefix
	DelayedMessagePrefix = []byte("espressoDelayed")
)

type DelayedMessageFetcher struct {
	fromBlock            uint64
	delayedBridge        *DelayedBridge
	sequencerInbox       *SequencerInbox
	l1Reader             *headerreader.HeaderReader
	maxBlocksToRead      uint64
	db                   ethdb.Database
	waitForFinalization  bool
	waitForConfirmations bool
	requiredBlockDepth   uint64
}

type DelayedMessageFetcherInterface interface {
	Start(ctx context.Context) bool
	storeDelayedMessageCount(db ethdb.Database, count uint64) error
	processDelayedMessage(messageWithMetadataAndPos *espressostreamer.MessageWithMetadataAndPos) (*espressostreamer.MessageWithMetadataAndPos, error)
}

/*
backFill fetches all the delayed messages till a `matureL1Block` which is within the saferty tolerance of the rollup
and stores them in the database
*/
func (d *DelayedMessageFetcher) backFill(ctx context.Context) error {
	log.Info("backfilling delayed messages")
	// Get the l1 block number based on the read mode
	matureL1Block, err := d.getL1BlockNumber(ctx)
	if err != nil {
		log.Error("Error getting l1 block number", "err", err)
		return err
	}
	log.Info("got l1 block number", "matureL1Block", matureL1Block)

	// Get the from block number from the delayed message fetcher
	// config. Note: Its important in the first read we read from the config
	// and not the database because the user might want to start reading from a `fromBlock`
	// which is before the delayed message number stored in the database
	fromBlock := d.fromBlock
	log.Info("getting delayed messages", "fromBlock", fromBlock, "matureL1Block", matureL1Block)
	batch := d.db.NewBatch()

	// Loop through the blocks until we reach the matureL1Block
	for fromBlock < matureL1Block {
		if (matureL1Block - fromBlock) > d.maxBlocksToRead {
			log.Info("getting delayed messages in range with max blocks to read", "fromBlock", fromBlock, "endBlock", fromBlock+d.maxBlocksToRead)
			// If the difference is greater than the maxBlocksToRead,
			// then set the endBlock to fromBlock + maxBlocksToRead
			err := d.getDelayedMessagesInRange(ctx, batch, fromBlock, fromBlock+d.maxBlocksToRead)
			if err != nil {
				log.Error("failed to get delayed messages in range", "err", err, "fromBlock", fromBlock, "endBlock", fromBlock+d.maxBlocksToRead)
				return err
			}
			fromBlock += d.maxBlocksToRead
		} else {
			// If the difference is less than the maxBlocksToRead,
			// then set the endBlock to matureL1Block
			err := d.getDelayedMessagesInRange(ctx, batch, fromBlock, matureL1Block)
			if err != nil {
				log.Error("failed to get delayed messages in range without maxblocks to read", "err", err, "fromBlock", fromBlock, "endBlock", matureL1Block)
				return err
			}
			fromBlock = matureL1Block
		}

	}

	log.Info("Backfilled delayed messages")
	err = batch.Write()
	if err != nil {
		return err
	}
	return nil
}

/*
startWatchDelayedMessages starts watching for new headers and processes them to get any new delayed messages
within the safety tolerance of the rollup
*/
func (d *DelayedMessageFetcher) startWatchDelayedMessages(ctx context.Context) {
	// Subscibe to new headers
	newHeaders, unsubscribe := d.l1Reader.Subscribe(false)
	defer unsubscribe()

	select {
	case header, ok := <-newHeaders:
		// If we get a new header, we need to backfill
		if !ok {
			log.Error("headerChan closed unexpectedly")
		} else {
			err := d.processNewHeader(ctx, header)
			if err != nil {
				log.Warn("could not process new header", "err", err, "header", header.Number.Uint64())
			}
		}

	case <-ctx.Done():
		log.Error("context done in delayed message fetcher", "err", ctx.Err())
		return
	}
}

/*
processNewHeader processes the new header to get any delayed messages
*/
func (d *DelayedMessageFetcher) processNewHeader(ctx context.Context, header *types.Header) error {
	var endBlock uint64
	var err error
	if endBlock, err = d.getL1BlockWithinSafetyTolerance(ctx, header); err != nil {
		log.Error("delayed message fetcher backfill failed", "err", err)
		return err
	}
	batch := d.db.NewBatch()
	// Get the from block from the database
	fromBlock, err := readCurrentFromBlockFromDb(d.db)
	if err != nil {
		log.Error("failed to read from block from db", "err", err)
		return err
	}
	err = d.getDelayedMessagesInRange(ctx, batch, fromBlock, endBlock)
	if err != nil {
		log.Error("failed to get delayed messages in range", "err", err, "fromBlock", fromBlock, "endBlock", endBlock)
		return err
	}
	err = batch.Write()
	if err != nil {
		return err
	}
	return nil
}

func (f *DelayedMessageFetcher) processDelayedMessage(messageWithMetadataAndPos *espressostreamer.MessageWithMetadataAndPos) (*espressostreamer.MessageWithMetadataAndPos, error) {
	delayedMessagesRead := messageWithMetadataAndPos.MessageWithMeta.DelayedMessagesRead

	// Get the delayed message count store in the database
	delayedCount, err := getDelayedMessageCount(f.db)
	if err != nil {
		log.Error("Failed to get delayed message count from db", "err", err)
		return nil, err
	}

	delayedMessageToProcess := delayedMessagesRead - 1

	if delayedMessageToProcess > delayedCount {
		log.Warn(("delayed message fetcher is lagging behind. delayedMessagesRead: %v, delayedCount: %v"), delayedMessagesRead, delayedCount)
		return nil, fmt.Errorf("delayed message fetcher is lagging behind")
	}
	log.Debug("Getting delayed message", "delayedCount", delayedMessageToProcess)
	// If this is delayed message, we need to get the message from L1
	// and replace the message in the messageWithMetadataAndPos
	// Note: here we are using DelayedMessagesRead - 1 because that is the index of the delayed message
	// that needs to be read
	message, err := f.readDelayedMessage(delayedMessageToProcess)
	if err != nil {
		log.Error("failed to get delayed message", "err", err)
		return messageWithMetadataAndPos, err
	}
	messageWithMetadataAndPos.MessageWithMeta.Message = message.Message

	return messageWithMetadataAndPos, nil
}

/***** Getter Functions *****/

/*
Reads the current from block from the database.
*/
func readCurrentFromBlockFromDb(db ethdb.Database) (uint64, error) {
	var blockNumber uint64
	blockNumberBytes, err := db.Get([]byte(DelayedFetcherCurrentFromBlockKey))
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

/*
Reads the delayed message from the database
*/
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

/*
getL1BlockNumber returns the L1 block number based on the config

	waitForFinalization - if true, it returns the latest finalized block number
	waitForConfirmations - if true, it returns the latest block number - requiredBlockDepth
	else - it returns the latest safe block number
*/
func (d *DelayedMessageFetcher) getL1BlockNumber(ctx context.Context) (uint64, error) {

	// If in setting we need to wait for finalized block, then get the latest finalized block number
	if d.waitForFinalization {
		return d.l1Reader.LatestFinalizedBlockNr(ctx)
	}

	// If we need to wait for confirmations,
	// then get the latest block number - requiredBlockDepth
	if d.waitForConfirmations {
		latestBlockNumber, err := d.l1Reader.Client().BlockNumber(ctx)
		if err != nil {
			return 0, err
		}
		// Get the latest block - requiredBlockDepth
		return latestBlockNumber - d.requiredBlockDepth, nil
	}

	// If no value is set, just use the latest block number
	return d.l1Reader.Client().BlockNumber(ctx)
}

/*
getDelayedMessagedInRange fetches all the delayed messages in the range [startBlock, endBlock]
and stores them in the database
*/
func (d *DelayedMessageFetcher) getDelayedMessagesInRange(ctx context.Context, batch ethdb.Batch, startBlock uint64, endBlock uint64) error {

	// Fetching the sequencer batches is important so that we can later parse the batch and get the sequencer batch data to store in the database
	log.Info("Looking for batches in range", "from", startBlock, "to", endBlock)

	// startBlock to bigInt

	startBlockBigInt := big.NewInt(0).SetUint64(startBlock)
	endBlockBigInt := big.NewInt(0).SetUint64(endBlock)
	log.Info("Looking for delayed batches from range", "from", startBlock, "to", endBlock)
	sequencerBatches, err := d.sequencerInbox.LookupBatchesInRange(ctx, startBlockBigInt, endBlockBigInt)
	if err != nil {
		return err
	}
	log.Info("Sequencer batches", "sequencerBatches", sequencerBatches)
	log.Info("Looking for delayed messages from range", "from", startBlock, "to", endBlock)

	msgs, err := d.delayedBridge.LookupMessagesInRange(ctx, big.NewInt(0).SetUint64(startBlock), big.NewInt(0).SetUint64(endBlock), func(batchNum uint64) ([]byte, error) {
		if len(sequencerBatches) > 0 && batchNum >= sequencerBatches[0].SequenceNumber {
			idx := batchNum - sequencerBatches[0].SequenceNumber
			if idx < uint64(len(sequencerBatches)) {
				return sequencerBatches[idx].Serialize(ctx, d.l1Reader.Client())
			}
			return nil, fmt.Errorf("failed to get sequencer batch data: %w", err)
		} else {
			return nil, fmt.Errorf("failed to get sequencer batch data: %w", err)
		}
	})
	if err != nil {
		log.Error("Failed to lookup delayed messages", "err", err)
		return err
	}

	log.Info("Sequencer delayed messages", "delayedMessages", msgs)

	// Get the delayed message count store in the database
	delayedCount, err := getDelayedMessageCount(d.db)
	if err != nil {
		log.Error("Failed to get delayed message count from db", "err", err)
		return err
	}

	log.Info("Delayed message count", "delayedCount", delayedCount)

	for _, msg := range msgs {
		seqNum, err := msg.Message.Header.SeqNum()
		if err != nil {
			return err
		}
		if seqNum > delayedCount+1 {
			// We need to panic the node here because something has gone seriously wrong
			log.Crit("Caff node is skipping delayed messages", "seqNum", seqNum, "delayedCount", delayedCount)
		}
		delayedCount++
		err = d.storeDelayedMessage(batch, delayedCount, *msg)
		if err != nil {
			return err
		}
	}
	log.Info("Delayed messages stored", "delayedMessages", msgs)
	// Store the from block in the database
	err = storeCurrentFromBlock(batch, endBlock)
	if err != nil {
		log.Error("failed to store current from block", "err", err, "fromBlock", endBlock)
		return err
	}
	log.Info("Current from block stored", "fromBlock", endBlock)
	return nil
}

// getDelayedMessageCount returns the delayed message count from the database
func getDelayedMessageCount(db ethdb.Database) (uint64, error) {
	var delayedCount uint64
	delayedCountBytes, err := db.Get([]byte(DelayedMessageCountKey))
	if err != nil {
		return 0, fmt.Errorf("failed to get delayed message count: %w", err)
	}
	err = rlp.DecodeBytes(delayedCountBytes, &delayedCount)
	if err != nil {
		return 0, fmt.Errorf("failed to decode delayed message count: %w", err)
	}
	return delayedCount, nil
}

/*
getL1BlockWithinSafetyTolerance checks if the L1 block is within the safety tolerance of the rollup
  - if we need to wait for finalized block, then it returns the latest finalized block number
  - if we need to wait for confirmations, then it returns the latest block number - requiredBlockDepth
  - else - it returns the latest header
*/
func (d *DelayedMessageFetcher) getL1BlockWithinSafetyTolerance(ctx context.Context, header *types.Header) (uint64, error) {
	fromBlock, err := readCurrentFromBlockFromDb(d.db)
	if err != nil {
		log.Error("failed to read from block from db", "err", err)
		return 0, err
	}
	// If we have already processed this header, we can skip it
	if header.Number.Uint64() < fromBlock {
		log.Warn("L1 block number is less than from block", "l1Block", header.Number.Uint64(), "fromBlock", fromBlock)
		return 0, errors.New("l1 block number is less than from block")
	}
	if d.waitForFinalization {
		// if we have configured to wait for finalizations, fetch the latest finalized block number.
		blockNumber, err := d.l1Reader.LatestFinalizedBlockNr(ctx)
		if err != nil {
			log.Warn("Error getting finalized block header to check safety tolerance of delayed message", "err", err)
			return 0, err
		}

		if blockNumber < fromBlock {
			log.Warn("L1 block number is less than from block", "l1Block", blockNumber, "fromBlock", fromBlock)
			return 0, errors.New("finalized block has already been processed")
		}
		return blockNumber, nil
	}
	if d.waitForConfirmations {
		// Get the block number which is latest header - requiredBlockDepth
		if header.Number.Uint64()-d.requiredBlockDepth < fromBlock {
			log.Warn("block already processed", "l1Block", header.Number.Uint64()-d.requiredBlockDepth, "fromBlock", fromBlock)
			return 0, errors.New("block already processed")
		}
		return header.Number.Uint64() - d.requiredBlockDepth, nil
	}
	return header.Number.Uint64(), nil
}

/***** Setter Functions *****/

/*
Stores the current from block in the database.
*/
func storeCurrentFromBlock(batch ethdb.Batch, fromBlock uint64) error {
	blockNumberBytes, err := rlp.EncodeToBytes(fromBlock)
	if err != nil {
		return fmt.Errorf("failed to encode next from block: %w", err)
	}

	err = batch.Put([]byte(DelayedFetcherCurrentFromBlockKey), blockNumberBytes)
	if err != nil {
		return fmt.Errorf("failed to put next from block: %w", err)
	}

	return nil
}

/*
Store the delayed message and delayed message count in the database
*/
func (f *DelayedMessageFetcher) storeDelayedMessage(batch ethdb.Batch, seqNum uint64, msg DelayedInboxMessage) error {
	key := dbKey(DelayedMessagePrefix, seqNum)
	encodedMsg, err := rlp.EncodeToBytes(msg)
	if err != nil {
		return fmt.Errorf("failed to encode delayed message: %w", err)
	}
	// Also update the delayed message count in the database
	err = f.storeDelayedMessageCount(f.db, seqNum)
	if err != nil {
		return err
	}

	return batch.Put(key, encodedMsg)
}

// storeDelayedMessageCount stores the delayed message count in the database
func (f *DelayedMessageFetcher) storeDelayedMessageCount(db ethdb.Database, count uint64) error {
	countBytes, err := rlp.EncodeToBytes(count)
	if err != nil {
		return fmt.Errorf("failed to encode delayed message count: %w", err)
	}
	return db.Put([]byte(DelayedMessageCountKey), countBytes)
}

/***** Initialization Function *****/
func NewDelayedMessageFetcher(
	delayedBridge *DelayedBridge,
	l1Reader *headerreader.HeaderReader,
	db ethdb.Database,
	blocksToRead uint64,
	waitForFinalization bool,
	waitForConfirmations bool,
	requiredBlockDepth uint64,
	fromBlock uint64,
	sequencerInbox *SequencerInbox,
) *DelayedMessageFetcher {

	return &DelayedMessageFetcher{
		fromBlock:            fromBlock,
		delayedBridge:        delayedBridge,
		l1Reader:             l1Reader,
		db:                   db,
		waitForFinalization:  waitForFinalization,
		waitForConfirmations: waitForConfirmations,
		requiredBlockDepth:   requiredBlockDepth,
		maxBlocksToRead:      blocksToRead,
		sequencerInbox:       sequencerInbox,
	}
}

/***** Start Function *****/

func (d *DelayedMessageFetcher) Start(ctx context.Context) bool {
	log.Info("starting delayed message fetcher")
	// Delayed message fetcher doesnt start until it has backfilled all the messages
	// till a `matureBlock` which is within the saferty tolerance of the rollup
	err := d.backFill(ctx)
	if err != nil {
		log.Error("delayed message fetcher backfill failed", "err", err)
		return false
	}
	// Start watching for delayed messages in a go routine: TODO: should we handle this correctly?
	go d.startWatchDelayedMessages(ctx)
	return true
}
