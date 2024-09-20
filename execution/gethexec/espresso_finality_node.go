package gethexec

import (
	"context"
	espressoClient "github.com/EspressoSystems/espresso-sequencer-go/client"
	espressoTypes "github.com/EspressoSystems/espresso-sequencer-go/types"
	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbos"
	"time"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

var (
	retryTime = time.Second * 1
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

func (s *HotsShotState) advance() {
	s.nextSeqBlockBum += 1
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

	arbTxns, err := n.hotshotState.client.FetchTransactionsInBlock(ctx, header.Height, n.namespace)
	if err != nil {
		arbos.LogFailedToFetchTransactions(header.Height, err)
		return false
	}

	arbHeader := &arbostypes.L1IncomingMessageHeader{
		Kind:        arbostypes.L1MessageType_L2Message,
		Poster:      l1pricing.BatchPosterAddress,
		BlockNumber: header.L1Head,
		Timestamp:   header.Timestamp,
		RequestId:   nil,
		L1BaseFee:   nil,
	}

	// Deserialize the transactions and ignore the malformed transactions
	txes := types.Transactions{}
	for _, tx := range arbTxns.Transactions {
		var out types.Transaction
		if err := out.UnmarshalBinary(tx); err != nil {
			log.Warn("Malformed tx is found")
			continue
		}
		txes = append(txes, &out)
	}

	hooks := arbos.NoopSequencingHooks()
	_, err = n.execEngine.SequenceTransactions(arbHeader, txes, hooks, false)
	if err != nil {
		log.Error("Espresso Finality Node: Failed to sequence transactions", "err", err)
		return false
	}
	n.hotshotState.advance()

	return true
}

func (n *EspressoFinalityNode) Start(ctx context.Context) error {
	n.StopWaiter.Start(ctx, n)
	n.CallIterativelySafe(func(ctx context.Context) time.Duration {
		madeBlock := n.createBlock(ctx)
		if madeBlock {
			return 0
		}
		return retryTime
	})
	return nil
}

func (n *EspressoFinalityNode) PublishTransaction(ctx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		return err
	}

	txn := espressoTypes.Transaction{
		Namespace: n.namespace,
		Payload:   txBytes,
	}
	if _, err := n.hotshotState.client.SubmitTransaction(ctx, txn); err != nil {
		log.Error("Espresso Finality Node: Failed to submit transaction", "err", err)
		return err
	}
	return nil
}

func (n *EspressoFinalityNode) CheckHealth(ctx context.Context) error {
	return nil
}

func (n *EspressoFinalityNode) Initialize(ctx context.Context) error {
	return nil
}
