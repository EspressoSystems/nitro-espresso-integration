package gethexec

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbstate"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

/*
Caff Node creates blocks with finalized hotshot transactions
*/
type CaffNode struct {
	stopwaiter.StopWaiter

	config           SequencerConfigFetcher
	executionEngine  *ExecutionEngine
	espressoStreamer *arbstate.EspressoStreamer
	skippedBlockPos  *uint64
}

func NewCaffNode(configFetcher SequencerConfigFetcher, execEngine *ExecutionEngine) *CaffNode {
	config := configFetcher()
	if err := config.Validate(); err != nil {
		log.Crit("Failed to validate caff  node config", "err", err)
	}

	espressoStreamer := arbstate.NewEspressoStreamer(config.CaffNodeConfig.Namespace,
		config.CaffNodeConfig.HotShotUrls,
		config.CaffNodeConfig.HotshotNextBlock,
		config.CaffNodeConfig.RetryTime,
		config.CaffNodeConfig.HotshotPollingInterval,
		config.CaffNodeConfig.BridgeAddr,
		config.CaffNodeConfig.ParentChainNodeUrl,
		config.CaffNodeConfig.ParentChainReader,
	)

	if espressoStreamer == nil {
		log.Crit("Failed to create espresso streamer")
	}

	return &CaffNode{
		config:           configFetcher,
		executionEngine:  execEngine,
		espressoStreamer: espressoStreamer,
	}
}

/**
 * This function will create a block with the finalized hotshot transactions
 * It will first remove duplicates and ensure the ordering of messages is correct
 * Then it will run the STF using the `Produce Block`function and finally store the block in the database
 */
func (n *CaffNode) createBlock() (returnValue bool) {

	// Get the last block header stored in the database
	if n.executionEngine.bc == nil {
		log.Error("execution engine bc not initialized")
		return false
	}

	lastBlockHeader := n.executionEngine.bc.CurrentBlock()

	currentMsgPos, err := n.executionEngine.BlockNumberToMessageIndex(lastBlockHeader.Number.Uint64())
	if err != nil {
		log.Error("failed to convert block number to message index")
		return false
	}
	currentPos := uint64(currentMsgPos)

	messageWithMetadataAndPos := n.espressoStreamer.PeekMessageWithMetadataAndPos()
	if messageWithMetadataAndPos == nil {
		log.Warn("No messages to process")
		return false
	}

	messageWithMetadata := messageWithMetadataAndPos.MessageWithMeta
	messageWithMetadataPos := messageWithMetadataAndPos.Pos

	// Check for duplicates and remove them
	if messageWithMetadataAndPos.Pos <= currentPos {
		log.Error("message has already been processed, removing duplicate",
			"messageWithMetadataPos", messageWithMetadataPos, "currentMessageCount", currentPos)
		n.espressoStreamer.PopMessageWithMetadataAndPos()
		return false
	}

	// Check if the message is in the correct order, it should be sequentially increasing
	if (messageWithMetadataPos != currentPos+1) && n.skippedBlockPos == nil {
		log.Warn("order of message is incorrect", "currentPos", currentPos,
			"messageWithMetadataPos", messageWithMetadataPos)
		return false
	}

	// If a message was skipped, check if the message is in the correct order
	if n.skippedBlockPos != nil && messageWithMetadataPos != *n.skippedBlockPos+1 {
		log.Warn("order of message is incorrect", "skippedBlockPos", *n.skippedBlockPos,
			"messageWithMetadataPos", messageWithMetadataPos)
		return false
	}

	// Get the state of the database at the last block
	statedb, err := n.executionEngine.bc.StateAt(lastBlockHeader.Root)
	if err != nil {
		log.Error("failed to get state at last block header", "err", err)
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
		n.executionEngine.bc,
		n.executionEngine.bc.Config(),
		false,
		core.MessageReplayMode)

	if err != nil {
		log.Error("Failed to produce block", "err", err)
		// if we fail to produce a block, we should remove this message from the queue
		// and set skippedBlockPos to the current messageWithMetadataPos
		n.skippedBlockPos = &messageWithMetadataPos
		log.Warn("Removing message from queue", "messageWithMetadataPos", messageWithMetadataPos)
		n.espressoStreamer.PopMessageWithMetadataAndPos()
		return false
	}

	// If block is nil or receipts is empty, return false
	if len(receipts) == 0 || block == nil {
		log.Error("Failed to produce block, no receipts or block")
		// if we fail to produce a block, we should remove this message from the queue
		// and set skippedBlockPos to the current messageWithMetadataPos
		n.skippedBlockPos = &messageWithMetadataPos
		log.Warn("Removing message from queue", "messageWithMetadataPos", messageWithMetadataPos)
		n.espressoStreamer.PopMessageWithMetadataAndPos()
		return false
	}

	// Reset the skippedBlockPos
	n.skippedBlockPos = nil

	blockCalcTime := time.Since(startTime)

	log.Info("Produced block", "block", block.Hash(), "blockNumber", block.Number(), "receipts", len(receipts))

	err = n.executionEngine.appendBlock(block, statedb, receipts, blockCalcTime)
	if err != nil {
		log.Error("Failed to append block", "err", err)
		return false
	}

	// Pop the message from the front of the queue at the end.
	n.espressoStreamer.PopMessageWithMetadataAndPos()
	return true
}

func (n *CaffNode) Start(ctx context.Context) error {
	n.StopWaiter.Start(ctx, n)
	err := n.espressoStreamer.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start espresso streamer: %w", err)
	}

	err = n.CallIterativelySafe(func(ctx context.Context) time.Duration {
		madeBlock := n.createBlock()
		if madeBlock {
			return n.config().CaffNodeConfig.HotshotPollingInterval
		}
		return n.config().CaffNodeConfig.RetryTime
	})
	if err != nil {
		return fmt.Errorf("failed to start node, error in createBlock: %w", err)
	}
	return nil
}

func (n *CaffNode) PublishTransaction(ctx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	return nil
}

func (n *CaffNode) CheckHealth(ctx context.Context) error {
	return nil
}

func (n *CaffNode) Initialize(ctx context.Context) error {
	return nil
}
