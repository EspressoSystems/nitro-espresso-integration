package arbnode

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/espressostreamer"
	"github.com/offchainlabs/nitro/util/headerreader"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

var (
	DelayedFetcherCurrentFromBlockKey          = []byte("espressoDelayedFetcherCurrentFromBlock")
	DelayedFetcherCurrentFromBlockSignatureKey = []byte("espressoDelayedFetcherCurrentFromBlockSignature")
	DelayedMessageCountKey                     = []byte("espressoDelayedMessageCount")
	// To not to mess with the existing schema, we use another prefix
	DelayedMessagePrefix = []byte("espressoDelayed")
)

type DelayedMessageFetcher struct {
	stopwaiter.StopWaiter

	fromBlock                uint64
	delayedBridge            *DelayedBridge
	sequencerInbox           *SequencerInbox
	l1Reader                 *headerreader.HeaderReader
	maxBlocksToRead          uint64
	waitForFinalization      bool
	waitForConfirmations     bool
	requiredBlockDepth       uint64
	fatalErrChan             chan error
	delayedCount             uint64
	delayedMessages          map[uint64]*DelayedInboxMessage
	delayedMessagesFromBlock map[uint64]uint64
}

type DelayedMessageFetcherInterface interface {
	Start(ctx context.Context) bool
	storeDelayedMessageLatestIndex(count uint64)
	processDelayedMessage(messageWithMetadataAndPos *espressostreamer.MessageWithMetadataAndPos) (*espressostreamer.MessageWithMetadataAndPos, uint64, error)
	getDelayedMessageLatestIndexAtBlock(blockNumber uint64) (uint64, error)
	StopAndWait()
}

var _ DelayedMessageFetcherInterface = new(DelayedMessageFetcher)

/*
backfill fetches all delayed messages until `matureL1Block` which is within the safety tolerance of the rollup
*/
func (d *DelayedMessageFetcher) backfill(ctx context.Context) error {
	// Get the l1 block number based on the read mode
	matureL1Block, err := d.getL1BlockNumber(ctx)
	if err != nil {
		log.Error("Error getting l1 block number", "err", err)
		return err
	}

	fromBlock := d.fromBlock
	log.Info("backfilling delayed messages", "fromBlock", fromBlock, "matureL1Block", matureL1Block)

	// Loop through the blocks until we reach the matureL1Block
	for fromBlock < matureL1Block {
		toBlock := matureL1Block
		// If the difference is greater than the maxBlocksToRead,
		// then set the endBlock to fromBlock + maxBlocksToRead
		if (matureL1Block - fromBlock) > d.maxBlocksToRead {
			toBlock = fromBlock + d.maxBlocksToRead
		}

		err := d.getDelayedMessagesInRange(ctx, fromBlock, toBlock)
		if err != nil {
			log.Error("failed to get delayed messages in range", "err", err, "fromBlock", fromBlock, "endBlock", toBlock)
			return err
		}
		fromBlock = toBlock + 1
	}

	log.Info("Backfilled delayed messages")
	return nil
}

/*
startWatchDelayedMessages starts watching for new headers and processes them to get any new delayed messages
within the safety tolerance of the rollup
*/
func (d *DelayedMessageFetcher) startWatchDelayedMessages(ctx context.Context) {
	log.Info("Starting watch for new headers in delayed message fetcher")
	// Subscibe to new headers

	if !d.l1Reader.Started() {
		// Start the l1 reader
		d.l1Reader.Start(ctx)
	}

	newHeaders, unsubscribe := d.l1Reader.Subscribe(false)

	d.LaunchThread(func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				log.Error("context done in delayed message fetcher", "err", ctx.Err())
				unsubscribe()
				return

			case header, ok := <-newHeaders:
				if !ok {
					log.Error("headerChan closed unexpectedly")
				} else {
					err := d.processNewHeader(ctx, header)
					if err != nil {
						log.Warn("could not process new header", "err", err, "header", header.Number.Uint64())
					}
				}
			}
		}
	})
}

/*
processNewHeader processes the new header to get any delayed messages
*/
func (d *DelayedMessageFetcher) processNewHeader(ctx context.Context, header *types.Header) error {
	log.Debug("Processing new header in delayed message fetcher", "header", header.Number.Uint64())
	var endBlock uint64
	var err error
	if endBlock, err = d.getL1BlockWithinSafetyTolerance(ctx, header); err != nil {
		return err
	}
	if endBlock == 0 {
		return nil
	}

	// Get the from block from the database
	fromBlock := d.fromBlock
	log.Debug("getting delayed messages in range", "fromBlock", fromBlock, "endBlock", endBlock)
	err = d.getDelayedMessagesInRange(ctx, fromBlock, endBlock)
	if err != nil {
		log.Error("failed to get delayed messages in range", "err", err, "fromBlock", fromBlock, "endBlock", endBlock)
		return err
	}

	return nil
}

func (d *DelayedMessageFetcher) processDelayedMessage(messageWithMetadataAndPos *espressostreamer.MessageWithMetadataAndPos) (*espressostreamer.MessageWithMetadataAndPos, uint64, error) {
	delayedMessagesRead := messageWithMetadataAndPos.MessageWithMeta.DelayedMessagesRead

	delayedCount := d.delayedCount
	// If this is delayed message, we need to get the message from L1
	// and replace the message in the messageWithMetadataAndPos
	delayedMessageToProcess := delayedMessagesRead - 1

	if delayedMessageToProcess > delayedCount {
		log.Warn("delayed message fetcher is lagging behind", "delayedMessagesRead", delayedMessagesRead, "delayedCount", delayedCount)
		return nil, 0, fmt.Errorf("delayed message fetcher is lagging behind")
	}
	log.Debug("Getting delayed message", "delayedCount", delayedMessageToProcess)

	// Note: here we are using DelayedMessagesRead - 1 because that is the index of the delayed message
	// that needs to be read
	message, fromBlock, err := d.readDelayedMessageAndFromBlock(delayedMessageToProcess)
	if err != nil {
		log.Error("failed to get delayed message", "err", err)
		return messageWithMetadataAndPos, 0, err
	}
	messageWithMetadataAndPos.MessageWithMeta.Message = message.Message

	return messageWithMetadataAndPos, fromBlock, nil
}

/*
Reads the delayed message from the database
*/
func (f *DelayedMessageFetcher) readDelayedMessageAndFromBlock(key uint64) (*DelayedInboxMessage, uint64, error) {
	if _, ok := f.delayedMessages[key]; !ok {
		return nil, 0, fmt.Errorf("delayed message not found")
	}
	delayedMessage := f.delayedMessages[key]
	if _, ok := f.delayedMessagesFromBlock[key]; !ok {
		return nil, 0, fmt.Errorf("delayed message from block not found")
	}
	delayedMessageFromBlock := f.delayedMessagesFromBlock[key]
	return delayedMessage, delayedMessageFromBlock, nil
}

/*
getL1BlockNumber returns the L1 block number based on the config.

	if waitForFinalization == true:
		return latest finalized block number
	else if waitForConfirmations == true:
		return latest block number - requiredBlockDepth
	else:
		return latest  block number
*/
func (d *DelayedMessageFetcher) getL1BlockNumber(ctx context.Context) (uint64, error) {

	// If in setting we need to wait for finalized block, then get the latest finalized block number
	if d.waitForFinalization {
		return d.l1Reader.LatestFinalizedBlockNr(ctx)
	} else if d.waitForConfirmations {
		// If we need to wait for confirmations,
		// then get the latest block number - requiredBlockDepth
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

// getDelayedMessageLatestIndexAtBlock is a wrapper function for the delayedBridge.GetMessageCount function. This allows users of the DelayedMessageFetcher
// to query for the message count at a block.
func (f *DelayedMessageFetcher) getDelayedMessageLatestIndexAtBlock(blockNumber uint64) (uint64, error) {
	count, err := f.delayedBridge.GetMessageCount(context.Background(), new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return 0, err
	}
	return count, nil
}

/*
getDelayedMessagedInRange fetches all the delayed messages in the range [startBlock, endBlock]
and stores them in the database
*/
func (d *DelayedMessageFetcher) getDelayedMessagesInRange(ctx context.Context, fromBlock uint64, toBlock uint64) error {

	// Fetching the sequencer batches is important so that we can later parse the batch and get the sequencer batch data to store in the database
	log.Debug("Looking for batches in range", "from", fromBlock, "to", toBlock)
	fromBlockBigInt := big.NewInt(0).SetUint64(fromBlock)
	toBlockBigInt := big.NewInt(0).SetUint64(toBlock)

	sequencerBatches, err := d.sequencerInbox.LookupBatchesInRange(ctx, fromBlockBigInt, toBlockBigInt)
	if err != nil {
		return err
	}
	log.Debug("Sequencer batches found", "sequencerBatches", sequencerBatches)

	log.Debug("Looking for delayed messages from range", "from", fromBlock, "to", toBlock)
	msgs, err := d.delayedBridge.LookupMessagesInRange(ctx, big.NewInt(0).SetUint64(fromBlock), big.NewInt(0).SetUint64(toBlock), func(batchNum uint64) ([]byte, error) {
		if len(sequencerBatches) > 0 && batchNum >= sequencerBatches[0].SequenceNumber {
			idx := batchNum - sequencerBatches[0].SequenceNumber
			if idx < uint64(len(sequencerBatches)) {
				return sequencerBatches[idx].Serialize(ctx, d.l1Reader.Client())
			}
			return nil, fmt.Errorf("failed to get sequencer batch data: %w", err)
		}

		return nil, fmt.Errorf("failed to get sequencer batch data: %w", err)
	})
	if err != nil {
		log.Error("Failed to lookup delayed messages", "err", err)
		return err
	}

	log.Debug("sequencer delayed messages found", "delayedMessages", msgs)

	lastDelayedMessageIndex := d.delayedCount

	for _, msg := range msgs {
		seqNum, err := msg.Message.Header.SeqNum()
		if err != nil {
			return err
		}
		log.Info("Caff node: processing delayed message", "seqNum", seqNum)
		if seqNum == 0 {
			// init message
			log.Debug("caff node: skip storing init message")
			continue
		}

		if seqNum <= lastDelayedMessageIndex {
			log.Warn("Caff node already has processed delayed message", "seqNum", seqNum, "lastDelayedMessageIndex", lastDelayedMessageIndex)
			continue
		}

		if seqNum > lastDelayedMessageIndex+1 {
			// Delayed message fetcher expects to fetch messages strictly in order. If a missing message is detected here,
			// it indicates a break in sequential fetching. Recovery is not handled by this fetcher and would require additional logic.
			err := fmt.Errorf("caff node is missing a delayed message with seqNum: %d, lastDelayedMessageIndex: %d", seqNum, lastDelayedMessageIndex)
			log.Error(err.Error())
			d.fatalErrChan <- err
			return err
		}

		lastDelayedMessageIndex++

		d.delayedMessages[lastDelayedMessageIndex] = msg
		d.delayedMessagesFromBlock[lastDelayedMessageIndex] = fromBlock
		d.delayedCount += 1
	}

	// Store the from block in the database
	d.fromBlock = toBlock + 1

	return nil
}

/*
getL1BlockWithinSafetyTolerance checks if the L1 block is within the safety tolerance of the rollup
  - if we need to wait for finalized block, then it returns the latest finalized block number
  - if we need to wait for confirmations, then it returns the latest block number - requiredBlockDepth
  - else - it returns the latest header
*/
func (d *DelayedMessageFetcher) getL1BlockWithinSafetyTolerance(ctx context.Context, header *types.Header) (uint64, error) {
	fromBlock := d.fromBlock
	// If we have already processed this header, we can skip it
	if header.Number.Uint64() < fromBlock {
		log.Warn("L1 block number is less than from block", "l1Block", header.Number.Uint64(), "fromBlock", fromBlock)
		return 0, nil
	}
	if d.waitForFinalization {
		// if we have configured to wait for finalizations, fetch the latest finalized block number.
		blockNumber, err := d.l1Reader.LatestFinalizedBlockNr(ctx)
		if err != nil {
			return 0, fmt.Errorf("error getting finalized block header to check safety tolerance of delayed message: %w", err)
		}

		if blockNumber < fromBlock {
			log.Warn("finalized block has already been processed", "current finalized block number", blockNumber, "fromBlock", fromBlock)
			return 0, nil
		}
		return blockNumber, nil
	} else if d.waitForConfirmations {
		// Get the block number which is latest header - requiredBlockDepth
		if header.Number.Uint64()-d.requiredBlockDepth < fromBlock {
			return 0, fmt.Errorf("block already processed current block number: %v, fromBlock: %v", header.Number.Uint64()-d.requiredBlockDepth, fromBlock)
		}
		return header.Number.Uint64() - d.requiredBlockDepth, nil
	}
	return header.Number.Uint64(), nil
}

// storeDelayedMessageLatestIndex stores the delayed message index in the database
func (d *DelayedMessageFetcher) storeDelayedMessageLatestIndex(count uint64) {
	d.delayedCount = count
}

func NewDelayedMessageFetcher(
	delayedBridge *DelayedBridge,
	l1Reader *headerreader.HeaderReader,
	blocksToRead uint64,
	waitForFinalization bool,
	waitForConfirmations bool,
	requiredBlockDepth uint64,
	fromBlock uint64,
	sequencerInbox *SequencerInbox,
	fatalErrChan chan error,
) *DelayedMessageFetcher {
	return &DelayedMessageFetcher{
		fromBlock:                fromBlock,
		delayedBridge:            delayedBridge,
		l1Reader:                 l1Reader,
		waitForFinalization:      waitForFinalization,
		waitForConfirmations:     waitForConfirmations,
		requiredBlockDepth:       requiredBlockDepth,
		maxBlocksToRead:          blocksToRead,
		sequencerInbox:           sequencerInbox,
		fatalErrChan:             fatalErrChan,
		delayedMessages:          make(map[uint64]*DelayedInboxMessage),
		delayedMessagesFromBlock: make(map[uint64]uint64),
	}
}

func (d *DelayedMessageFetcher) Start(ctx context.Context) bool {
	log.Info("Starting delayed message fetcher")
	d.StopWaiter.Start(ctx, d)
	// Delayed message fetcher doesnt start until it has backfilled all the messages
	// till a `matureBlock` which is within the saferty tolerance of the rollup
	err := d.backfill(ctx)
	if err != nil {
		log.Error("delayed message fetcher backfill failed", "err", err)
		return false
	}
	d.startWatchDelayedMessages(ctx)
	return true
}

func (d *DelayedMessageFetcher) StopAndWait() {
	d.StopWaiter.StopAndWait()
}
