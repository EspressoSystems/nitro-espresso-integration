package arbnode

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/util/arbmath"
	"github.com/offchainlabs/nitro/util/headerreader"
	"github.com/offchainlabs/nitro/util/stopwaiter"
	flag "github.com/spf13/pflag"
)

type ForceInclusionCheckerConfig struct {
	RetryTime                time.Duration `koanf:"retry-time"`
	PollingInterval          time.Duration `koanf:"polling-interval"`
	BlockThresholdTolerance  uint64        `koanf:"block-threshold-tolerance"`
	SecondThresholdTolerance uint64        `koanf:"second-threshold-tolerance"`
}

var DefaultEspressoForceInclusionCheckerConfig = ForceInclusionCheckerConfig{
	RetryTime:                time.Second * 2,
	PollingInterval:          time.Second * 100,
	BlockThresholdTolerance:  20,
	SecondThresholdTolerance: 200,
}

func EspressoForceInclusionConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Duration(prefix+".retry-time", DefaultEspressoForceInclusionCheckerConfig.RetryTime, "retry time after a failure")
	f.Duration(prefix+".polling-interval", DefaultEspressoForceInclusionCheckerConfig.PollingInterval, "time after a success")
	f.Uint64(prefix+".block-threshold-tolerance", DefaultEspressoForceInclusionCheckerConfig.BlockThresholdTolerance, "block threshold tolerance")
	f.Uint64(prefix+".second-threshold-tolerance", DefaultEspressoForceInclusionCheckerConfig.SecondThresholdTolerance, "second threshold tolerance")
}

// SeqInboxInterface defines an interface for interacting with the sequencer inbox contract.
// Note: When `deployBold` is disabled, the [MaxTimeVariation](arbnode/espresso_force_inclusion_checker.go:14:1-14:78) values are hardcoded,
// which makes this interface difficult to mock in tests.
type SeqInboxInterface interface {
	MaxTimeVariation(context.Context) (*big.Int, *big.Int, *big.Int, *big.Int, error)
	TotalDelayedMessagesRead(context.Context) (*big.Int, error)
}

type SeqInbox struct {
	seqInbox *bridgegen.SequencerInbox
}

func (s *SeqInbox) MaxTimeVariation(ctx context.Context) (*big.Int, *big.Int, *big.Int, *big.Int, error) {
	return s.seqInbox.MaxTimeVariation(&bind.CallOpts{Context: ctx})
}

func (s *SeqInbox) TotalDelayedMessagesRead(ctx context.Context) (*big.Int, error) {
	return s.seqInbox.TotalDelayedMessagesRead(&bind.CallOpts{Context: ctx})
}

type ForceInclusionChecker struct {
	stopwaiter.StopWaiter

	seqInbox              SeqInboxInterface
	config                ForceInclusionCheckerConfig
	l1Reader              *headerreader.HeaderReader
	delayedMessageFetcher *DelayedMessageFetcher
	fatalErrChan          chan error
}

func NewForceInclusionChecker(
	seqInbox SeqInboxInterface,
	config ForceInclusionCheckerConfig,
	l1Reader *headerreader.HeaderReader,
	delayedMessageFetcher *DelayedMessageFetcher,
	fatalErrChan chan error,
) *ForceInclusionChecker {
	return &ForceInclusionChecker{
		seqInbox:              seqInbox,
		config:                config,
		l1Reader:              l1Reader,
		delayedMessageFetcher: delayedMessageFetcher,
		fatalErrChan:          fatalErrChan,
	}
}

func (f *ForceInclusionChecker) checkIfMessageCanBeForceIncluded(ctx context.Context) error {
	// Get the total number of delayed messages read in the sequencer inbox
	totalDelayedMessagesRead, err := f.seqInbox.TotalDelayedMessagesRead(ctx)
	if err != nil {
		return fmt.Errorf("error getting total delayed messages read: %w", err)
	}

	// Get the earliest block number that is without the force inclusion tolerance
	badBlockNumber := f.getForceInclusionToleranceBlockNumber(ctx)
	// Check the delayed message count at this block number
	count, err := f.delayedMessageFetcher.getDelayedMessageCountAtBlock(badBlockNumber)
	if err != nil {
		return fmt.Errorf("error getting delayed message count at block %d: %w", badBlockNumber, err)
	}
	// If the message count in delay inbox is less than or equal to the total delayed messages read
	// then no force inclusion is going to happen.
	if count <= arbmath.BigToUintSaturating(totalDelayedMessagesRead) {
		return nil
	}
	// Force inclusion is going to happen, panic the node.
	err = fmt.Errorf("force inclusion is going to happen")
	f.fatalErrChan <- err
	return err
}

func (f *ForceInclusionChecker) Start(ctx context.Context) error {
	f.StopWaiter.Start(ctx, f)

	return f.CallIterativelySafe(func(ctx context.Context) time.Duration {
		err := f.checkIfMessageCanBeForceIncluded(ctx)
		if err != nil {
			log.Error("error checking force inclusion", "err", err)
			return f.config.RetryTime
		}
		return f.config.PollingInterval
	})
}

func (f *ForceInclusionChecker) getForceInclusionToleranceBlockNumber(ctx context.Context) uint64 {
	maxTimeVariationDelayBlocks, _, maxTimeVariationDelaySeconds, _, err := f.seqInbox.MaxTimeVariation(ctx)
	if err != nil {
		return 0
	}
	currentParentChainBlock, err := f.l1Reader.Client().BlockByNumber(ctx, nil)
	if err != nil {
		return 0
	}

	lastBadBlockNumber := arbmath.SaturatingUSub(f.config.BlockThresholdTolerance+currentParentChainBlock.NumberU64(), arbmath.BigToUintSaturating(maxTimeVariationDelayBlocks))
	lastBadBlockTime := arbmath.SaturatingUSub(f.config.SecondThresholdTolerance+currentParentChainBlock.Time(), arbmath.BigToUintSaturating(maxTimeVariationDelaySeconds))

	lastBadBlock := f.findFirstParentChainBlockBelow(ctx, lastBadBlockNumber, lastBadBlockTime)
	return lastBadBlock
}

func (f *ForceInclusionChecker) findFirstParentChainBlockBelow(ctx context.Context, lastBadBlockNumber uint64, lastBadBlockTime uint64) uint64 {
	client := f.l1Reader.Client()
	blockNumber := lastBadBlockNumber

	for blockNumber > 0 {
		block, err := client.BlockByNumber(ctx, arbmath.UintToBig(blockNumber))
		if err != nil {
			return 0
		}
		if block.NumberU64() <= lastBadBlockNumber || block.Time() <= lastBadBlockTime {
			return block.NumberU64()
		}
		blockNumber--
	}
	return 0
}
