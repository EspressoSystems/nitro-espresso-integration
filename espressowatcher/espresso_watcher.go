package espressowatcher

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/util/headerreader"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type EspressoWatcher struct {
	stopwaiter.StopWaiter
	retryTime               time.Duration
	pollingInterval         time.Duration
	l1Reader                *headerreader.HeaderReader
	sequencerInbox          bridgegen.SequencerInbox
	blockThresholdTolerance uint64
	blockThresholdTimestamp uint64
}

func NewEspressoWatcher(l1Reader *headerreader.HeaderReader, retryTime, pollingInterval time.Duration, sequencerInboxAddress common.Address, blockThresholdTolerance uint64, blockThresholdTimestamp uint64) *EspressoWatcher {

	sequencerInbox, err := bridgegen.NewSequencerInbox(sequencerInboxAddress, l1Reader.Client())
	if err != nil || sequencerInbox == nil {
		log.Crit("Failed to create sequencer inbox", "err", err)
	}

	return &EspressoWatcher{
		l1Reader:                l1Reader,
		retryTime:               retryTime,
		pollingInterval:         pollingInterval,
		sequencerInbox:          *sequencerInbox,
		blockThresholdTolerance: blockThresholdTolerance,
		blockThresholdTimestamp: blockThresholdTimestamp,
	}
}

func (w *EspressoWatcher) getFirstBlockBelowThreshold(ctx context.Context, currentFinalizedBlockNum uint64, currentFinalizedBlockTime uint64) (*big.Int, error) {

	return nil, nil
}

func (w *EspressoWatcher) getForceInclusionToleranceBlockNumber(ctx context.Context, currentFinalizedBlockNum uint64, currentFinalizedBlockTime uint64) (*big.Int, error) {

	delayBlocks, _, delaySeconds, _, err := w.sequencerInbox.MaxTimeVariation(&bind.CallOpts{Context: ctx, BlockNumber: big.NewInt(int64(currentFinalizedBlockNum))})
	if err != nil || delayBlocks == nil || delaySeconds == nil {
		return nil, fmt.Errorf("error getting max time variation: %w", err)
	}
	firstEligibleBlockNumber := big.NewInt(int64(currentFinalizedBlockNum)).Sub(big.NewInt(int64(currentFinalizedBlockNum)), delayBlocks)
	firstEligibleTimestamp := big.NewInt(int64(currentFinalizedBlockTime)).Sub(big.NewInt(int64(currentFinalizedBlockTime)), delaySeconds)

	firstBufferedEligibleBlockNumber := firstEligibleBlockNumber.Add(firstEligibleBlockNumber, big.NewInt(int64(w.blockThresholdTolerance)))
	firstBufferedEligibleBlockTimestamp := firstEligibleTimestamp.Add(firstEligibleTimestamp, big.NewInt(int64(w.blockThresholdTimestamp)))
	log.Info("first eligible block number and timestamp", "firstEligibleBlockNumber", firstBufferedEligibleBlockNumber, "firstEligibleTimestamp", firstBufferedEligibleBlockTimestamp)

	return w.getFirstBlockBelowThreshold(ctx, currentFinalizedBlockNum, currentFinalizedBlockTime)
}

func (w *EspressoWatcher) isForceInclusionPossibleSoon(ctx context.Context, delayedMessagesRead big.Int, currentFinalizedBlockNum uint64, currentFinalizedBlockTime uint64) bool {

	firstBufferedEligibleBlockNumber, err := w.getForceInclusionToleranceBlockNumber(ctx, currentFinalizedBlockNum, currentFinalizedBlockTime)
	if err != nil {
		log.Error("error getting force inclusion tolerance block number", "err", err)
		return false
	}
	log.Info("first eligible block number and timestamp", "firstEligibleBlockNumber", firstBufferedEligibleBlockNumber)
	// Now loop backwards until you find the delayed message

	return false
}

func (w *EspressoWatcher) checkIfMessageCanBeForceIncluded(ctx context.Context) error {
	finalizedHeader, err := w.l1Reader.LatestFinalizedBlockHeader(ctx)
	if err != nil {
		return err
	}
	currentFinalizedBlockNum := arbutil.ParentHeaderToL1BlockNumber(finalizedHeader)
	currentFinalizedBlockTimestamp := finalizedHeader.Time
	delayedMessagesRead, err := w.sequencerInbox.TotalDelayedMessagesRead(&bind.CallOpts{Context: ctx, BlockNumber: big.NewInt(int64(currentFinalizedBlockNum))})
	if err != nil || delayedMessagesRead == nil {
		return fmt.Errorf("error getting delayed messages read: %w", err)
	}
	isPossible := w.isForceInclusionPossibleSoon(ctx, *delayedMessagesRead, currentFinalizedBlockNum, currentFinalizedBlockTimestamp)
	if isPossible {
		log.Crit("Force inclusion possible soon", "delayedMessagesRead", delayedMessagesRead)
	}
	return nil
}

func (w *EspressoWatcher) Start(ctxIn context.Context) error {
	w.StopWaiter.Start(ctxIn, w)
	err := w.CallIterativelySafe(func(ctx context.Context) time.Duration {
		err := w.checkIfMessageCanBeForceIncluded(ctx)
		if err != nil {
			log.Error("error checking if message can be force included", "err", err)
			return w.retryTime
		}
		return w.pollingInterval
	})

	return err
}
