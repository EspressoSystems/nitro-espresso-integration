// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package gethexec

import (
	"context"
	"encoding/json"
	"strings"
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
	retryTime = time.Second * 1
)

type HotShotState struct {
	client          espressoClient.Client
	nextSeqBlockNum uint64
}

func NewHotShotState(log log.Logger, url string, startBlock uint64) *HotShotState {
	return &HotShotState{
		client:          *espressoClient.NewClient(log, url),
		nextSeqBlockNum: startBlock,
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

func removeWhitespace(s string) string {
	// Split the string on whitespace then concatenate the segments
	return strings.Join(strings.Fields(s), "")
}

func NewEspressoSequencer(execEngine *ExecutionEngine, configFetcher SequencerConfigFetcher) (*EspressoSequencer, error) {
	config := configFetcher()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &EspressoSequencer{
		execEngine:   execEngine,
		config:       configFetcher,
		hotShotState: NewHotShotState(log.New(), config.HotShotUrl, config.StartHotShotBlock),
		namespace:    config.EspressoNamespace,
	}, nil
}

func (s *EspressoSequencer) createBlock(ctx context.Context) (returnValue bool) {
	if s.hotShotState.nextSeqBlockNum == 0 {
		latestBlock, err := s.hotShotState.client.FetchLatestBlockHeight(ctx)
		//if err != nil || latestBlock == 0 {
		if err != nil {
			log.Warn("Unable to fetch the latest hotshot block")
			return false
		}
		log.Info("Starting sequencing at the latest hotshot block", "block number", latestBlock)
		s.hotShotState.nextSeqBlockNum = latestBlock
	}
	nextSeqBlockNum := s.hotShotState.nextSeqBlockNum
	var err error
	// header, err := s.hotShotState.client.FetchHeaderByHeight(ctx, nextSeqBlockNum)
	// if err != nil {
	// 	log.Warn("Unable to fetch header for block number, will retry", "block_num", nextSeqBlockNum)
	// 	return false
	// }
	data := []byte(removeWhitespace(`{
		"height": 42,
		"timestamp": 789,
		"l1_head": 124,
		"l1_finalized": {
			"number": 123,
			"timestamp": "0x456",
			"hash": "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		},
		"ns_table": {
			"raw_payload":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]
		},
		"payload_commitment": "HASH~1yS-KEtL3oDZDBJdsW51Pd7zywIiHesBZsTbpOzrxOfu",
		"block_merkle_tree_root": "MERKLE_COMM~yB4_Aqa35_PoskgTpcCR1oVLh6BUdLHIs7erHKWi-usUAAAAAAAAAAEAAAAAAAAAJg",
		"fee_merkle_tree_root": "MERKLE_COMM~VJ9z239aP9GZDrHp3VxwPd_0l28Hc5KEAB1pFeCIxhYgAAAAAAAAAAIAAAAAAAAAdA"
	}`))

	// Check encoding.
	// Check decoding
	var header espressoTypes.Header
	if err := json.Unmarshal(data, &header); err != nil {
		log.Error(("failed to decode"))
		return false
	}

	// arbTxns, err := s.hotShotState.client.FetchTransactionsInBlock(ctx, header.Height, s.namespace)
	jsonString := `{"NonExistence":{"ns_id":0}}`
	var rawJson json.RawMessage = json.RawMessage(jsonString)
	transactions := make([]espressoTypes.Bytes, 0)

	var arbTxns = espressoClient.TransactionsInBlock{
		Transactions: transactions,
		Proof:        rawJson,
	}
	// if err != nil {
	// 	log.Error("Error fetching transactions", "err", err)
	// 	return false

	// }

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

	log.Info("here is the jst", "jst", string(jst.Proof))

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
	var txnBytes, err = tx.MarshalBinary()
	if err != nil {
		return err
	}
	txn := espressoTypes.Transaction{
		Vm:      s.namespace,
		Payload: txnBytes,
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
