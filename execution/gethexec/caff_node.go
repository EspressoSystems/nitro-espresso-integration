package gethexec

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/espressostreamer"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

/*
Caff Node creates blocks with finalized hotshot transactions
*/
type CaffNode struct {
	stopwaiter.StopWaiter

	config           SequencerConfigFetcher
	executionEngine  *ExecutionEngine
	espressoStreamer *espressostreamer.EspressoStreamer
	l2Client         *ethclient.Client
}

func NewCaffNode(configFetcher SequencerConfigFetcher, execEngine *ExecutionEngine) *CaffNode {
	config := configFetcher()
	if err := config.Validate(); err != nil {
		log.Crit("Failed to validate caff  node config", "err", err)
	}

	espressoStreamer := espressostreamer.NewEspressoStreamer(config.CaffNodeConfig.Namespace,
		config.CaffNodeConfig.HotShotUrls,
		config.CaffNodeConfig.NextHotshotBlock,
		config.CaffNodeConfig.RetryTime,
		config.CaffNodeConfig.HotshotPollingInterval,
		config.CaffNodeConfig.ParentChainNodeUrl,
		config.CaffNodeConfig.ParentChainReader,
		config.CaffNodeConfig.EspressoTEEVerifierAddr,
	)

	if espressoStreamer == nil {
		log.Crit("Failed to create espresso streamer")
	}

	var l2Client *ethclient.Client
	if config.CaffNodeConfig.SequencerUrl != "" {
		ethClient, err := ethclient.Dial(config.CaffNodeConfig.SequencerUrl)
		if err != nil {
			log.Crit("Failed to connect to Ethereum client: %v", err)
		}
		l2Client = ethClient
	}

	return &CaffNode{
		config:           configFetcher,
		executionEngine:  execEngine,
		espressoStreamer: espressoStreamer,
		l2Client:         l2Client,
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

	messageWithMetadataAndPos, err := n.espressoStreamer.Next()
	if err != nil || messageWithMetadataAndPos == nil {
		log.Warn("unable to get next message", "err", err)
		return false
	}

	messageWithMetadata := messageWithMetadataAndPos.MessageWithMeta

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
		return false
	}

	// If block is nil or receipts is empty, return false
	if block == nil {
		log.Error("Failed to produce block, no receipts or block")
		return false
	}

	blockCalcTime := time.Since(startTime)

	log.Info("Produced block", "block", block.Hash(), "blockNumber", block.Number(), "receipts", len(receipts))

	err = n.executionEngine.appendBlock(block, statedb, receipts, blockCalcTime)
	if err != nil {
		log.Error("Failed to append block", "err", err)
		log.Info("Resetting espresso streamer", "currentMessagePos",
			messageWithMetadataAndPos.Pos, "currentHostshotBlock",
			messageWithMetadataAndPos.HotshotHeight)
		n.espressoStreamer.Reset(messageWithMetadataAndPos.Pos, messageWithMetadataAndPos.HotshotHeight)
		return false
	}

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
	if n.l2Client != nil {
		err := n.l2Client.SendTransaction(ctx, tx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *CaffNode) CheckHealth(ctx context.Context) error {
	return nil
}

func (n *CaffNode) Initialize(ctx context.Context) error {
	return nil
}
