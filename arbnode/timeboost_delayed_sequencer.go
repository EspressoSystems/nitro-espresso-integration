// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbnode

import (
	"context"

	flag "github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/log"

	decentralizedtimeboost "github.com/offchainlabs/nitro/espresso/timeboost"
	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type TimeboostDelayedSequencer struct {
	stopwaiter.StopWaiter
	inbox  *InboxTracker
	reader *InboxReader
	exec   execution.ExecutionSequencer
	config TimeboostDelayedSequencerConfigFetcher
}

type TimeboostDelayedSequencerConfig struct {
	Enable bool `koanf:"enable" reload:"hot"`
}

type TimeboostDelayedSequencerConfigFetcher func() *TimeboostDelayedSequencerConfig

func TimeboostDelayedSequencerConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Bool(prefix+".enable", DefaultTimeboostDelayedSequencerConfig.Enable, "enable delayed sequencer")
}

var DefaultTimeboostDelayedSequencerConfig = TimeboostDelayedSequencerConfig{
	Enable: false,
}

var TestTimeboostDelayedSequencerConfig = TimeboostDelayedSequencerConfig{
	Enable: false,
}

func NewTimeboostDelayedSequencer(reader *InboxReader, exec execution.ExecutionSequencer, config TimeboostDelayedSequencerConfigFetcher) (*TimeboostDelayedSequencer, error) {
	d := &TimeboostDelayedSequencer{
		inbox:  reader.Tracker(),
		reader: reader,
		exec:   exec,
		config: config,
	}
	return d, nil
}

func (d *TimeboostDelayedSequencer) getDelayedMessagesRead() (uint64, error) {
	return d.exec.NextDelayedMessageNumber()
}

func (d *TimeboostDelayedSequencer) SequenceDelayedMessages(ctx context.Context, delayedCount uint64) ([]*decentralizedtimeboost.TimeboostDelayedMessage, error) {
	config := d.config()
	if !config.Enable {
		return nil, nil
	}

	startPos, err := d.getDelayedMessagesRead()
	if err != nil {
		return nil, err
	}

	// Retrieve all finalized delayed messages
	pos := startPos
	var messages []*decentralizedtimeboost.TimeboostDelayedMessage
	for pos < delayedCount {
		msg, _, _, err := d.inbox.GetDelayedMessageAccumulatorAndParentChainBlockNumber(ctx, pos)
		if err != nil {
			return nil, err
		}
		err = msg.FillInBatchGasCost(func(batchNum uint64) ([]byte, error) {
			data, _, err := d.reader.GetSequencerMessageBytes(ctx, batchNum)
			return data, err
		})
		if err != nil {
			return nil, err
		}
		delayedMsg := &decentralizedtimeboost.TimeboostDelayedMessage{
			Message: msg,
			Pos:     pos + 1,
		}
		messages = append(messages, delayedMsg)
		pos++
	}

	// Sequence the delayed messages, if any
	if len(messages) > 0 {
		for i, msg := range messages {
			// #nosec G115
			err = d.exec.SequenceDelayedMessage(msg.Message, startPos+uint64(i))
			if err != nil {
				return nil, err
			}
		}
		log.Info("DelayedSequencer: Sequenced", "msgnum", len(messages), "startpos", startPos)
	}

	return messages, nil
}
