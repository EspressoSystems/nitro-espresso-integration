// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package gethexec

import (
	"context"
	"encoding/binary"
	"time"

	"github.com/offchainlabs/nitro/arbos/espresso"
	"github.com/offchainlabs/nitro/util/stopwaiter"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
)

var (
	retryTime = time.Second * 5
)

type HotShotState struct {
	client          espresso.Client
	nextSeqBlockNum uint64
}

func NewHotShotState(log log.Logger, url string, namespace uint64) *HotShotState {
	return &HotShotState{
		client: *espresso.NewClient(log, url, namespace),
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
}

func NewEspressoSequencer(execEngine *ExecutionEngine, configFetcher SequencerConfigFetcher) (*EspressoSequencer, error) {
	config := configFetcher()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &EspressoSequencer{
		execEngine:   execEngine,
		config:       configFetcher,
		hotShotState: NewHotShotState(log.New(), config.HotShotUrl, config.EspressoNamespace),
	}, nil
}

func (s *EspressoSequencer) createBlock(ctx context.Context) (returnValue bool) {
	nextSeqBlockNum := s.hotShotState.nextSeqBlockNum
	header, err := s.hotShotState.client.FetchHeader(ctx, nextSeqBlockNum)
	if err != nil {
		log.Warn("Unable to fetch header for block number, will retry", "block_num", nextSeqBlockNum)
		return false
	}
	arbTxns, err := s.hotShotState.client.FetchTransactionsInBlock(ctx, nextSeqBlockNum, &header)
	if err != nil {
		log.Error("Error fetching transactions", "err", err)
		return false

	}

	if len(arbTxns.Transactions) == 0 {
		s.hotShotState.advance()
		return true
	}

	arbHeader := &arbostypes.L1IncomingMessageHeader{
		Kind:        arbostypes.L1MessageType_L2Message,
		Poster:      l1pricing.BatchPosterAddress,
		BlockNumber: header.L1Head,
		Timestamp:   header.Timestamp,
		RequestId:   nil,
		L1BaseFee:   nil,
		BlockJustification: &arbostypes.EspressoBlockJustification{
			Header: header,
			Proof:  arbTxns.Proof,
		},
	}

	msg := messageFromEspresso(arbHeader, arbTxns)
	_, err = s.execEngine.SequenceTransactionsEspresso(msg)
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
	if err := s.hotShotState.client.SubmitTransaction(parentCtx, tx); err != nil {
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

// messageFromEspresso serializes raw data from the espresso block into an arbitrum message,
// including malformed and invalid transactions.
// This allows validators to rebuild a block and check the espresso commitment.
//
// Note that the raw data is actually in JSON format, which can result in a larger size than necessary.
// Storing it in L1 call data would lead to some waste. However, for the sake of this Proof of Concept,
// this is deemed acceptable. Addtionally, after we finish the integration, there is no need to store
// message in L1.
//
// Refer to `execution/gethexec/executionengine.go messageFromTxes`
func messageFromEspresso(header *arbostypes.L1IncomingMessageHeader, txesInBlock espresso.TransactionsInBlock) arbostypes.L1IncomingMessage {
	var l2Message []byte

	txes := txesInBlock.Transactions
	if len(txes) == 1 {
		l2Message = append(l2Message, arbos.L2MessageKind_EspressoTx)
		l2Message = append(l2Message, txes[0]...)
	} else {
		l2Message = append(l2Message, arbos.L2MessageKind_Batch)
		sizeBuf := make([]byte, 8)
		for _, tx := range txes {
			binary.BigEndian.PutUint64(sizeBuf, uint64(len(tx)+1))
			l2Message = append(l2Message, sizeBuf...)
			l2Message = append(l2Message, arbos.L2MessageKind_EspressoTx)
			l2Message = append(l2Message, tx...)
		}

	}

	return arbostypes.L1IncomingMessage{
		Header: header,
		L2msg:  l2Message,
	}
}
