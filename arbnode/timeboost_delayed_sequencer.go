// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbnode

import (
	"context"

	protos "github.com/EspressoSystems/timeboost-proto/go-generated"
	"github.com/fxamacker/cbor/v2"
	flag "github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	decentralized_timeboost_types "github.com/offchainlabs/nitro/decentralized-timeboost/types"
	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type DecentralizedTimeboostDelayedSequencer struct {
	stopwaiter.StopWaiter
	inbox  *InboxTracker
	reader *InboxReader
	exec   execution.ExecutionSequencer
	config DecentralizedTimeboostDelayedSequencerConfigFetcher
}

type DecentralizedTimeboostDelayedSequencerConfig struct {
	Enable bool `koanf:"enable" reload:"hot"`
}

type DecentralizedTimeboostDelayedSequencerConfigFetcher func() *DecentralizedTimeboostDelayedSequencerConfig

func TimeboostDelayedSequencerConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Bool(prefix+".enable", DefaultTimeboostDelayedSequencerConfig.Enable, "enable delayed sequencer")
}

var DefaultTimeboostDelayedSequencerConfig = DecentralizedTimeboostDelayedSequencerConfig{
	Enable: false,
}

var TestTimeboostDelayedSequencerConfig = DecentralizedTimeboostDelayedSequencerConfig{
	Enable: false,
}

func NewDecentralizedTimeboostDelayedSequencer(
	reader *InboxReader,
	exec execution.ExecutionSequencer,
	config DecentralizedTimeboostDelayedSequencerConfigFetcher,
) (*DecentralizedTimeboostDelayedSequencer, error) {
	d := &DecentralizedTimeboostDelayedSequencer{
		inbox:  reader.Tracker(),
		reader: reader,
		exec:   exec,
		config: config,
	}
	return d, nil
}

func (d *DecentralizedTimeboostDelayedSequencer) getDelayedMessagesRead() (uint64, error) {
	return d.exec.NextDelayedMessageNumber()
}

func (d *DecentralizedTimeboostDelayedSequencer) createDelayedMessagesProtoBlock(
	messages []*arbostypes.L1IncomingMessage,
	startPos uint64,
	currentHeight uint64,
	round uint64,
) ([]*protos.Block, error) {
	i := uint64(1)
	var blocks []*protos.Block
	for _, msg := range messages {
		pos := currentHeight + i
		msg.L2msg = []byte{}
		messageWithMeta := arbostypes.MessageWithMetadata{
			Message:             msg,
			DelayedMessagesRead: startPos + i,
		}

		msgBytes, err := rlp.EncodeToBytes(messageWithMeta)
		if err != nil {
			return nil, err
		}
		payload := decentralized_timeboost_types.MessagePayload{
			Position: pos,
			Message:  msgBytes,
		}
		encoded, err := cbor.Marshal(payload)
		if err != nil {
			return nil, err
		}
		block := &protos.Block{
			Number:  pos,
			Round:   round,
			Payload: encoded,
		}
		blocks = append(blocks, block)
		i++
	}
	return blocks, nil
}

func (d *DecentralizedTimeboostDelayedSequencer) SequenceDecentralizedTimeboostDelayedMessages(
	ctx context.Context,
	currentHeight uint64,
	delayedCount uint64,
	round uint64,
) ([]*protos.Block, error) {
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
	var messages []*arbostypes.L1IncomingMessage
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
		messages = append(messages, msg)
		pos++
	}

	// Sequence the delayed messages, if any
	if len(messages) > 0 {
		for i, msg := range messages {
			// #nosec G115
			err = d.exec.SequenceDelayedMessage(msg, startPos+uint64(i))
			if err != nil {
				return nil, err
			}
		}
		log.Info("DecentralizedTimeboostDelayedSequencer: Sequenced", "msgnum", len(messages), "startpos", startPos, "current block num", currentHeight)
	}
	return d.createDelayedMessagesProtoBlock(messages, startPos, currentHeight, round)
}
