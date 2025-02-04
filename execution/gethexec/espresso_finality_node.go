package gethexec

import (
	"context"
	"fmt"
	"sync"
	"time"

	espressoClient "github.com/EspressoSystems/espresso-sequencer-go/client"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

var (
	retryTime = time.Second * 1
)

/*
Espresso Finality Node creates blocks with finalized hotshot transactions
*/
type EspressoFinalityNode struct {
	stopwaiter.StopWaiter

	config               SequencerConfigFetcher
	namespace            uint64
	executionEngine      *ExecutionEngine
	espressoClient       *espressoClient.Client
	nextSeqBlockNum      uint64
	messagesWithMetadata []*arbostypes.MessageWithMetadata
	messagesStateMutex   sync.Mutex
}

func NewEspressoFinalityNode(configFetcher SequencerConfigFetcher, execEngine *ExecutionEngine) *EspressoFinalityNode {
	config := configFetcher()
	if err := config.Validate(); err != nil {
		panic(err)
	}
	return &EspressoFinalityNode{
		config:          configFetcher,
		namespace:       config.EspressoFinalityNodeConfig.Namespace,
		espressoClient:  espressoClient.NewClient(config.EspressoFinalityNodeConfig.HotShotUrl),
		nextSeqBlockNum: config.EspressoFinalityNodeConfig.StartBlock,
		executionEngine: execEngine,
	}
}

// TODO: For future versions, we should check the attestation quote to check if its from a valid TEE
// TODO: Check for duplicates to make sure we don't double process the same message

/**
 * This function will create a block with the finalized hotshot transactions
 */
func (n *EspressoFinalityNode) createBlock(ctx context.Context) (returnValue bool) {
	//  If we have no messages to process, return
	if len(n.messagesWithMetadata) == 0 {
		log.Info("No messages to process")
		return false
	}
	n.messagesStateMutex.Lock()
	defer n.messagesStateMutex.Unlock()
	messageWithMetadata := n.messagesWithMetadata[0]
	lastBlockHeader := n.executionEngine.bc.CurrentBlock()
	// Get state
	statedb, err := n.executionEngine.bc.StateAt(lastBlockHeader.Root)
	if err != nil {
		log.Error("failed to get state at last block header", "err", err)
		return false
	}
	log.Info("Initial State", "lastBlockHash", lastBlockHeader.Hash(), "lastBlockStateRoot", lastBlockHeader.Root)
	startTime := time.Now()
	block, receipts, err := arbos.ProduceBlock(messageWithMetadata.Message, messageWithMetadata.DelayedMessagesRead, lastBlockHeader, statedb, n.executionEngine.bc, n.executionEngine.bc.Config(), false, core.MessageReplayMode)
	if err != nil {
		log.Error("Failed to produce block", "err", err)
		return false
	}
	if len(receipts) == 0 || block == nil {
		log.Error("Failed to produce block, no receipts or block")
		return false
	}
	blockCalcTime := time.Since(startTime)
	log.Info("Produced block", "block", block.Hash(), "blockNumber", block.Number(), "receipts", len(receipts))

	// Pop the message from the front of the queue
	n.messagesWithMetadata = n.messagesWithMetadata[1:]

	// add this transaction to the transacton streamer
	// so that they can be read by inbox_tracker to be further sent to the sequencer feed using
	// `PopulateFeedBacklog` function
	msgResult, err := n.executionEngine.resultFromHeader(block.Header())
	if err != nil {
		log.Error("Failed to get result from header", "err", err)
		return false
	}
	pos, err := n.executionEngine.BlockNumberToMessageIndex(lastBlockHeader.Number.Uint64() + 1)
	if err != nil {
		log.Error("Failed to get pos from last block header", "err", err)
		return false
	}
	err = n.executionEngine.consensus.WriteMessageFromSequencer(pos, *messageWithMetadata, *msgResult)
	if err != nil {
		log.Error("Failed to write message from sequencer", "err", err)
		return false
	}

	// TODO: think about this? I am not sure if this is the right way to do it
	// Only write the block after we've written the messages, so if the node dies in the middle of this,
	// it will naturally recover on startup by regenerating the missing block.
	err = n.executionEngine.appendBlock(block, statedb, receipts, blockCalcTime)
	if err != nil {
		log.Error("Failed to append block", "err", err)
		return false
	}
	return true
}

func (n *EspressoFinalityNode) queueMessagesFromHotshot(ctx context.Context) error {

	if n.nextSeqBlockNum == 0 {
		latestBlock, err := n.espressoClient.FetchLatestBlockHeight(ctx)
		if err != nil {
			log.Warn("unable to fetch latest hotshot block", "err", err)
			return err
		}
		log.Info("Started espresso finality node at the latest hotshot block", "block number", latestBlock)
		n.nextSeqBlockNum = latestBlock
	}

	nextSeqBlockNum := n.nextSeqBlockNum
	header, err := n.espressoClient.FetchHeaderByHeight(ctx, nextSeqBlockNum)
	if err != nil {
		log.Warn("failed to fetch header", "err", err)
		return err
	}

	height := header.Header.GetBlockHeight()
	arbTxns, err := n.espressoClient.FetchTransactionsInBlock(ctx, height, n.namespace)
	if err != nil {
		arbos.LogFailedToFetchTransactions(height, err)
		return err
	}
	n.messagesStateMutex.Lock()
	defer n.messagesStateMutex.Unlock()
	for _, tx := range arbTxns.Transactions {
		// Parse hotshot payload
		_, _, messages, err := arbutil.ParseHotShotPayload(tx)
		if err != nil {
			log.Warn("failed to parse hotshot payload, will retry", "err", err)
			return err
		}

		// Parse the messages
		for _, message := range messages {
			var messageWithMetadata arbostypes.MessageWithMetadata
			err = rlp.DecodeBytes(message, &messageWithMetadata)
			if err != nil {
				log.Warn("failed to decode message, will retry", "err", err)
				return err
			}
			n.messagesWithMetadata = append(n.messagesWithMetadata, &messageWithMetadata)
		}
	}
	return nil
}

func (n *EspressoFinalityNode) Start(ctx context.Context) error {
	n.StopWaiter.Start(ctx, n)

	err := n.CallIterativelySafe(func(ctx context.Context) time.Duration {
		err := n.queueMessagesFromHotshot(ctx)
		if err != nil {
			return retryTime
		}
		return 0
	})
	if err != nil {
		return fmt.Errorf("failed to start espresso finality node, error in queueMessagesFromHotshot: %w", err)
	}

	err = n.CallIterativelySafe(func(ctx context.Context) time.Duration {
		madeBlock := n.createBlock(ctx)
		if madeBlock {
			n.nextSeqBlockNum += 1
			return 0
		}
		return retryTime
	})
	if err != nil {
		return fmt.Errorf("failed to start espresso finality node, error in createBlock: %w", err)
	}
	return nil
}

func (n *EspressoFinalityNode) PublishTransaction(ctx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	return nil
}

func (n *EspressoFinalityNode) CheckHealth(ctx context.Context) error {
	return nil
}

func (n *EspressoFinalityNode) Initialize(ctx context.Context) error {
	return nil
}
