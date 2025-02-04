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

	config                  SequencerConfigFetcher
	namespace               uint64
	executionEngine         *ExecutionEngine
	espressoClient          *espressoClient.Client
	nextSeqBlockNum         uint64
	messagesWithMetadata    []*arbostypes.MessageWithMetadata
	messagesWithMetadataPos []uint64
	messagesStateMutex      sync.Mutex
}

func NewEspressoFinalityNode(configFetcher SequencerConfigFetcher, execEngine *ExecutionEngine) *EspressoFinalityNode {
	config := configFetcher()
	if err := config.Validate(); err != nil {
		log.Crit("Failed to validate espresso finality node config", "err", err)
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
func (n *EspressoFinalityNode) createBlock() (returnValue bool) {

	n.messagesStateMutex.Lock()
	defer n.messagesStateMutex.Unlock()
	//  If we have no messages to process, return
	if len(n.messagesWithMetadata) == 0 {
		log.Warn("No messages to process")
		return false
	}
	messageWithMetadata := n.messagesWithMetadata[0]
	messageWithMetadataPos := n.messagesWithMetadataPos[0]

	// Get the last block header stored in the database
	lastBlockHeader := n.executionEngine.bc.CurrentBlock()
	statedb, err := n.executionEngine.bc.StateAt(lastBlockHeader.Root)
	if err != nil {
		log.Error("failed to get state at last block header", "err", err)
		return false
	}
	log.Info("Initial State", "lastBlockHash", lastBlockHeader.Hash(), "lastBlockStateRoot", lastBlockHeader.Root)
	startTime := time.Now()

	// Run the Produce block function in replay mode
	// This is the core function that is used by replay.wasm to validate the block
	block, receipts, err := arbos.ProduceBlock(messageWithMetadata.Message, messageWithMetadata.DelayedMessagesRead, lastBlockHeader, statedb, n.executionEngine.bc, n.executionEngine.bc.Config(), false, core.MessageReplayMode)
	if err != nil {
		log.Error("Failed to produce block", "err", err)
		return false
	}

	// If block is nil or receipts is empty, return false
	if len(receipts) == 0 || block == nil {
		log.Error("Failed to produce block, no receipts or block")
		return false
	}
	blockCalcTime := time.Since(startTime)

	log.Info("Produced block", "block", block.Hash(), "blockNumber", block.Number(), "receipts", len(receipts))

	// add this transaction to the transacton streamer
	// so that this can be read by inbox_tracker to be further sent to the sequencer feed using
	// `PopulateFeedBacklog` function
	// This implementation only works, if we are using a tamper resistant database
	msgResult, err := n.executionEngine.resultFromHeader(block.Header())
	if err != nil {
		log.Error("Failed to get result from header", "err", err)
		return false
	}

	err = n.executionEngine.consensus.WriteMessageFromSequencer(arbutil.MessageIndex(messageWithMetadataPos), *messageWithMetadata, *msgResult)
	if err != nil {
		log.Error("Failed to write message from sequencer", "err", err)
		return false
	}

	if err != nil {
		log.Error("Failed to broadcast messages", "err", err)
		return false
	}

	log.Info("Broadcasted messages", "message", messageWithMetadataPos)

	// Only if we have sent the block to the sequencer feed, we can then write the block to the database
	err = n.executionEngine.appendBlock(block, statedb, receipts, blockCalcTime)
	if err != nil {
		log.Error("Failed to append block", "err", err)
		return false
	}

	// Pop the message from the front of the queue at the end.
	n.messagesWithMetadata = n.messagesWithMetadata[1:]
	n.messagesWithMetadataPos = n.messagesWithMetadataPos[1:]

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
		log.Warn("failed to fetch transactions", "err", err)
		return err
	}
	n.messagesStateMutex.Lock()
	defer n.messagesStateMutex.Unlock()
	for _, tx := range arbTxns.Transactions {
		// Parse hotshot payload
		_, indices, messages, err := arbutil.ParseHotShotPayload(tx)
		if err != nil {
			log.Warn("failed to parse hotshot payload, will retry", "err", err)
			return err
		}

		// Parse the messages
		for i, message := range messages {
			var messageWithMetadata arbostypes.MessageWithMetadata
			err = rlp.DecodeBytes(message, &messageWithMetadata)
			if err != nil {
				log.Warn("failed to decode message, will retry", "err", err)
				return err
			}
			n.messagesWithMetadata = append(n.messagesWithMetadata, &messageWithMetadata)
			n.messagesWithMetadataPos = append(n.messagesWithMetadataPos, indices[i])
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
		madeBlock := n.createBlock()
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
