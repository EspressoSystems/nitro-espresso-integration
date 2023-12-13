// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package gethexec

import (
	"context"
	"encoding/json"
	"time"

	"github.com/offchainlabs/nitro/util/stopwaiter"

	espressoClient "github.com/EspressoSystems/espresso-sequencer-go/client"
	espressoTypes "github.com/EspressoSystems/espresso-sequencer-go/types"
	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
)

var (
	retryTime = time.Second * 5
)

type HotShotState struct {
	client          espressoClient.Client
	nextSeqBlockNum uint64
}

func NewHotShotState(log log.Logger, url string) *HotShotState {
	return &HotShotState{
		client: *espressoClient.NewClient(log, url),
		// TODO: Load this from the inbox reader so that new sequencers don't read redundant blocks
		// https://github.com/EspressoSystems/espresso-sequencer/issues/734
		nextSeqBlockNum: 0,
	}
}

func (s *HotShotState) advance() {
	s.nextSeqBlockNum += 1
}

type EspressoSequencer struct {
	stopwaiter.StopWaiter

	execEngine   *ExecutionEngine
	config       SequencerConfigFetcher
	hotShotState *HotShotState
	namespace    uint64
}

func NewEspressoSequencer(execEngine *ExecutionEngine, configFetcher SequencerConfigFetcher) (*EspressoSequencer, error) {
	config := configFetcher()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &EspressoSequencer{
		execEngine:   execEngine,
		config:       configFetcher,
		hotShotState: NewHotShotState(log.New(), config.HotShotUrl),
		namespace:    config.EspressoNamespace,
	}, nil
}

func (s *EspressoSequencer) createBlock(ctx context.Context) (returnValue bool) {
	nextSeqBlockNum := s.hotShotState.nextSeqBlockNum
	header, err := s.hotShotState.client.FetchHeader(ctx, nextSeqBlockNum)
	if err != nil {
		log.Warn("Unable to fetch header for block number, will retry", "block_num", nextSeqBlockNum)
		return false
	}
	arbTxns, err := s.hotShotState.client.FetchTransactionsInBlock(ctx, &header, s.namespace)
	if err != nil {
		log.Error("Error fetching transactions", "err", err)
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

	jst := &arbostypes.EspressoBlockJustification{
		Header: header,
		Proof:  arbTxns.Proof,
	}

	_, err = s.execEngine.SequenceTransactionsEspresso(arbHeader, arbTxns.Transactions, jst)
	if err != nil {
		log.Error("Sequencing error for block number", "block_num", nextSeqBlockNum, "err", err)
		return false
	}

	s.hotShotState.advance()

	return true

}

func (s *EspressoSequencer) Start(ctxIn context.Context) error {
	s.StopWaiter.Start(ctxIn, s)
	s.CallIteratively(func(ctx context.Context) time.Duration {
		retryBlockTime := time.Now().Add(retryTime)
		madeBlock := s.createBlock(ctx)
		if madeBlock {
			// Allow the sequencer to catch up to HotShot
			return 0
		}
		// If we didn't make a block, try again in a bit
		return time.Until(retryBlockTime)
	})

	return nil
}

// Required methods for the TransactionPublisher interface
func (s *EspressoSequencer) PublishTransaction(parentCtx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	var txnBytes, err = json.Marshal(tx)
	if err != nil {
		return err
	}
	//	json.RawMessage is a []byte array, which is marshalled as a base64-encoded string.
	//	Our sequencer API expects a JSON array.
	payload := make([]uint8, len(txnBytes))
	for i := range payload {
		payload[i] = uint8(txnBytes[i])
	}
	txn := espressoTypes.Transaction{
		Vm:      s.namespace,
		Payload: payload,
	}
	if err := s.hotShotState.client.SubmitTransaction(parentCtx, txn); err != nil {
		log.Error("Failed to submit transaction", err)
		return err
	}
	return nil
}

func (s *EspressoSequencer) CheckHealth(ctx context.Context) error {
	return nil
}

func (s *EspressoSequencer) Initialize(ctx context.Context) error {
	return nil
}
