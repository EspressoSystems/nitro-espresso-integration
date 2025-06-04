package arbnode

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/solgen/go/node_interfacegen"
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

	ErrorToleranceDuration time.Duration `koanf:"error-tolerance-duration"`
}

var DefaultEspressoForceInclusionCheckerConfig = ForceInclusionCheckerConfig{
	RetryTime:                time.Second * 2,
	PollingInterval:          time.Second * 100,
	BlockThresholdTolerance:  20,
	SecondThresholdTolerance: 200,
	ErrorToleranceDuration:   time.Hour * 1,
}

func EspressoForceInclusionConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Duration(prefix+".retry-time", DefaultEspressoForceInclusionCheckerConfig.RetryTime, "retry time after a failure")
	f.Duration(prefix+".polling-interval", DefaultEspressoForceInclusionCheckerConfig.PollingInterval, "time after a success")
	f.Uint64(prefix+".block-threshold-tolerance", DefaultEspressoForceInclusionCheckerConfig.BlockThresholdTolerance, "block threshold tolerance")
	f.Uint64(prefix+".second-threshold-tolerance", DefaultEspressoForceInclusionCheckerConfig.SecondThresholdTolerance, "second threshold tolerance")
	f.Duration(prefix+".error-tolerance-duration", DefaultEspressoForceInclusionCheckerConfig.ErrorToleranceDuration, "error tolerance duration")
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
	badBlockNumber, err := f.getForceInclusionToleranceBlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("error getting force inclusion tolerance block number: %w", err)
	}
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
	var firstErrFound time.Time

	return f.CallIterativelySafe(func(ctx context.Context) time.Duration {
		err := f.checkIfMessageCanBeForceIncluded(ctx)
		if err != nil {
			if firstErrFound.IsZero() {
				firstErrFound = time.Now()
			} else if time.Since(firstErrFound) > f.config.ErrorToleranceDuration {
				f.fatalErrChan <- err
			}
			log.Error("error checking force inclusion", "err", err)
			return f.config.RetryTime
		}
		firstErrFound = time.Time{}
		return f.config.PollingInterval
	})
}

func (f *ForceInclusionChecker) getForceInclusionToleranceBlockNumber(ctx context.Context) (uint64, error) {
	maxTimeVariationDelayBlocks, _, maxTimeVariationDelaySeconds, _, err := f.seqInbox.MaxTimeVariation(ctx)
	if err != nil {
		return 0, err
	}

	parentLatestHeader, err := f.l1Reader.LastHeader(ctx)
	if err != nil {
		return 0, err
	}

	l1BlockNumber := parentLatestHeader.Number.Uint64()
	l1TimeStamp := parentLatestHeader.Time

	if f.l1Reader.IsParentChainArbitrum() {
		headerInfo := types.DeserializeHeaderExtraInformation(parentLatestHeader)
		l1BlockNumber = headerInfo.L1BlockNumber
	}

	lastBadBlockNumber := arbmath.SaturatingUSub(f.config.BlockThresholdTolerance+l1BlockNumber, arbmath.BigToUintSaturating(maxTimeVariationDelayBlocks))
	lastBadBlockTime := arbmath.SaturatingUSub(f.config.SecondThresholdTolerance+l1TimeStamp, arbmath.BigToUintSaturating(maxTimeVariationDelaySeconds))

	if f.l1Reader.IsParentChainArbitrum() {
		n, err := node_interfacegen.NewNodeInterface(types.NodeInterfaceAddress, f.l1Reader.Client())
		if err != nil {
			return 0, err
		}
		rng, err := n.L2BlockRangeForL1(&bind.CallOpts{Context: ctx}, lastBadBlockNumber)
		if err == nil {
			lastBadBlockNumber = rng.LastBlock
		} else {
			genesis, err := n.NitroGenesisBlock(&bind.CallOpts{Context: ctx})
			if err != nil {
				return 0, err
			}
			target := lastBadBlockNumber
			start := genesis.Uint64()
			end := parentLatestHeader.Number.Uint64()
			lastBadBlockNumber, err = binarySearchForBlockNumber(ctx, start, end, func(ctx context.Context, blockNumber uint64) (int, error) {
				block, err := f.l1Reader.Client().BlockByNumber(ctx, arbmath.UintToBig(blockNumber))
				if err != nil {
					return 0, err
				}
				l1Block := types.DeserializeHeaderExtraInformation(block.Header()).L1BlockNumber
				if l1Block < target {
					return binarySearch_LessThanTarget, nil
				} else if l1Block > target {
					return binarySearch_GreaterThanTarget, nil
				} else {
					return binarySearch_EqualToTarget, nil
				}
			})
			if err != nil {
				return 0, err
			}
		}
	}

	lastBadBlock := f.findFirstParentChainBlockBelow(ctx, lastBadBlockNumber, lastBadBlockTime)
	return lastBadBlock, nil
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
