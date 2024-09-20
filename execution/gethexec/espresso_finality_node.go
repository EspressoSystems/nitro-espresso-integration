package gethexec

import (
	"context"
	espressoClient "github.com/EspressoSystems/espresso-sequencer-go/client"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type HotsShotState struct {
	client          espressoClient.Client
	nextSeqBlockBum uint64
}

func NewHotsShotState(url string, startBlock uint64) *HotsShotState {
	return &HotsShotState{
		client:          *espressoClient.NewClient(url),
		nextSeqBlockBum: startBlock,
	}
}

type EspressoFinalityNode struct {
	stopwaiter.StopWaiter

	config       SequencerConfigFetcher
	execEngine   *ExecutionEngine
	hotshotState *HotsShotState
	namespace    uint64
}

func NewEspressoFinalityNode(execEngine *ExecutionEngine, configFetcher SequencerConfigFetcher) *EspressoFinalityNode {
	config := configFetcher()
	if err := config.Validate(); err != nil {
		panic(err)
	}
	return &EspressoFinalityNode{
		execEngine:   execEngine,
		config:       configFetcher,
		namespace:    config.EspressoFinaliyNode.Namespace,
		hotshotState: NewHotsShotState(config.EspressoFinaliyNode.HotShotUrl, config.EspressoFinaliyNode.StartBlock),
	}
}

func (n *EspressoFinalityNode) createBlock(ctx context.Context) (returnValue bool) {

	if n.hotshotState.nextSeqBlockBum == 0 {
		latestBlock, err := n.hotshotState.client.FetchLatestBlockHeight(ctx)
		if err != nil && latestBlock == 0 {
			log.Warn("unable to fetch latest hotshot block", "err", err)
			return false
		}
		log.Info("Started espresso finality node at the latest hotshot block", "block number", latestBlock)
		n.hotshotState.nextSeqBlockBum = latestBlock
	}

	nextSeqBlockNum := n.hotshotState.nextSeqBlockBum
	header, err := n.hotshotState.client.FetchHeaderByHeight(ctx, nextSeqBlockNum)
	if err != nil {
		arbos.LogFailedToFetchHeader(nextSeqBlockNum)
		return false
	}

	arbT

	return false
}
