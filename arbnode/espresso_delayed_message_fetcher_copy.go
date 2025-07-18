package arbnode

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/util/dbutil"
	"github.com/offchainlabs/nitro/util/headerreader"
)

type DelayedMessageFetcherCopy struct {
	fromBlock            uint64
	delayedBridge        *DelayedBridge
	sequencerInbox       *SequencerInbox
	client               *ethclient.Client
	l1Reader             *headerreader.HeaderReader
	delayedCount         uint64
	checkDelay           time.Duration
	minBlocksToRead      uint64
	defaultBlocksToRead  uint64
	targetMessagesRead   uint64
	maxBlocksToRead      uint64
	readMode             string
	db                   ethdb.Database
	waitForFinalization  bool
	waitForConfirmations bool
	requiredBlockDepth   uint64
}

func (d *DelayedMessageFetcherCopy) readDelayedMessageCount(db ethdb.Database) (uint64, error) {
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

func (d *DelayedMessageFetcherCopy) backFill(ctx context.Context) error {

	delayedMessageCountInDb, err := readDelayedMessageCount(d.db)
	if err != nil {
		return err
	}

	// Get the l1 block number based on the read mode
	matureL1Block, err := d.getL1BlockNumber(ctx)
	if err != nil {
		log.Error("Error getting l1 block number", "err", err)
		return err
	}

	// Get the from block number
	fromBlock := d.fromBlock

	// Loop through the blocks until we reach the matureL1Block
	for fromBlock <= matureL1Block {
		if (matureL1Block - fromBlock) > d.maxBlocksToRead {
			// If the difference is greater than the maxBlocksToRead,
			// then set the fromBlock to fromBlock + maxBlocksToRead
			fromBlock += d.maxBlocksToRead
		} else {

			// If the difference is less than the maxBlocksToRead,
			// then set the fromBlock to matureL1Block
			fromBlock = matureL1Block

		}

	}
	// Set the fromBlock to the new fromBlock
	// TODO: save the from block to the database
	return nil
}

func (f *DelayedMessageFetcher) getDelayedMessage(index uint64) (*arbostypes.L1IncomingMessage, error) {
	// Check if the delayed message at index exists in the database
	msg, err := f.readDelayedMessage(index)
	if err != nil && !dbutil.IsErrNotFound(err) {
		log.Error("Failed to read delayed message", "err", err, "msg", msg)
		return nil, err
	}
	// If the delayed message already exists in the database and we have already processed it
	// the parent block number then we can just return the message
	if msg != nil && f.fromBlock >= msg.ParentChainBlockNumber {
		log.Debug("Delayed message already exists in the database and we have already processed it", "msg", msg.ParentChainBlockNumber, "fromBlock", f.fromBlock)
		return msg.Message, nil
	}

	// get the current block number from the L1 Reader
	currL1, err := f.l1Reader.Client().BlockNumber(context.Background())
	if err != nil {
		return nil, err
	}
	// if the current L1 block is less than the from block this means no new L1 blocks have been added
	// since we did the last read, so we can just return nil
	if currL1 < f.fromBlock {
		return nil, fmt.Errorf("l1 block number %d is less than from block %d", currL1, f.fromBlock)
	}

	log.Debug("Current L1 block and from block:", "currL1", currL1, "fromBlock", f.fromBlock)

	startBlock := f.fromBlock
	endBlock := currL1
	hasFound := false

	batch := f.db.NewBatch()
	// Lookup `MessageDelivered` events from the `startBlock` to `startBlock + blocksToRead`
	// see if any of them have a `message` field that matches the `seqNum` we are looking for
	for startBlock <= endBlock && !hasFound {
		from := big.NewInt(0).SetUint64(startBlock)
		to := big.NewInt(0).SetUint64(startBlock + f.blocksToRead)

		log.Debug("Looking for delayed messages from range", "from", from, "to", to)
		msgs, err := f.delayedBridge.LookupMessagesInRange(context.Background(), from, to, nil)
		if err != nil {
			log.Error("Failed to lookup delayed messages", "err", err)
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
		// Read the next `blocksToRead` blocks
		startBlock = startBlock + f.blocksToRead + 1
	}

	// if startBlock is less than the endBlock this means
	// we were able to find the delayed message number before the endBlock
	// so next time, we start from where we left off
	if startBlock <= endBlock {
		f.fromBlock = startBlock
	} else {
		f.fromBlock = endBlock + 1
	}

	err = storeCurrentL1Block(batch, f.fromBlock)
	if err != nil {
		log.Error("Failed to store current L1 block", "err", err)
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
		log.Error("Failed to read delayed message", "err", err)
		return nil, err
	}

	return result.Message, nil
}
func (d *DelayedMessageFetcherCopy) getL1BlockNumber(ctx context.Context) (uint64, error) {

	// If in setting we need to wait for finalized block, then get the latest finalized block number
	if d.waitForFinalization {
		return d.l1Reader.LatestFinalizedBlockNr(ctx)
	}

	// If in setting we need to wait for confirmations,
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

func (d *DelayedMessageFetcherCopy) Start(ctx context.Context) bool {
	err := d.backFill(ctx)
	if err != nil {
		log.Error("delayed message fetcher backfill failed", "err", err)
		return false
	}
}
