package gethexec

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/offchainlabs/nitro/util/stopwaiter"
	flag "github.com/spf13/pflag"
)

type SailfishInclusionRound struct {
	roundId             uint64
	transactions        []*types.Transaction
	delayedMessagesRead uint64
	consesnsusTimestamp uint64
}

type TimeboostTransactionQueueItem struct {
	tx                 *types.Transaction
	roundId            uint64
	consensusTimestamp uint64
}

type TimeboostSequencer struct {
	stopwaiter.StopWaiter
	config         TimeboostSequencerConfigFetcher
	sailfishRounds []*SailfishInclusionRound
	execEngine     *ExecutionEngine
	txRetryQueue   []TimeboostTransactionQueueItem
}

type TimeboostSequencerConfigFetcher func() *TimeboostSequencerConfig

type TimeboostSequencerConfig struct {
	Enable        bool          `koanf:"enable"`
	MaxBlockSpeed time.Duration `koanf:"max-block-speed"`
}

var DefaultTimeboostSequencerConfig = TimeboostSequencerConfig{
	Enable:        true,
	MaxBlockSpeed: time.Millisecond * 250,
}

func TimeboostSequencerConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Bool(prefix+".enable", DefaultTimeboostSequencerConfig.Enable, "enable timeboost sequencer")
}

func NewTimeboostSequencer(config TimeboostSequencerConfig) (*TimeboostSequencer, error) {
	return &TimeboostSequencer{
		sailfishRounds: []*SailfishInclusionRound{},
	}, nil
}

func (s *TimeboostSequencer) createBlock(ctx context.Context) bool {

	// First we need to create the current list of transactions that we will process
	// We will do this by getting the transactions from the txRetryQueue to see
	// if an older round id stil has to be processed
	var txs []TimeboostTransactionQueueItem

	for i := 0; i < len(s.txRetryQueue); i++ {
		if s.txRetryQueue[i].roundId < s.sailfishRounds[0].roundId {
			txs = append(txs, s.txRetryQueue[i])
		}
	}

	return true

}

func (s *TimeboostSequencer) Start(ctx context.Context) error {
	s.StopWaiter.Start(ctx, s)
	nextBlock := time.Now().Add(s.config().MaxBlockSpeed)
	s.CallIterativelySafe(func(ctx context.Context) time.Duration {
		if s.createBlock(ctx) {
			return 0
		}
		return nextBlock.Sub(time.Now())
	})
	return nil
}
