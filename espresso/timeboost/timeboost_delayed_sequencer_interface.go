package decentralizedtimeboost

import (
	"context"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
)

type TimeboostDelayedMessage struct {
	Message *arbostypes.L1IncomingMessage
	Pos     uint64
}

type TimeboostDelayedSequencerInterface interface {
	SequenceDelayedMessages(ctx context.Context, delayedCount uint64) ([]*TimeboostDelayedMessage, error)
}
