package decentralized_timeboost

import (
	"context"

	protos "github.com/EspressoSystems/timeboost-proto/go-generated"
)

type DecentralizedTimeboostDelayedSequencerInterface interface {
	SequenceDelayedMessages(
		ctx context.Context,
		currentHeight uint64,
		delayedCount uint64,
		round uint64,
	) ([]*protos.Block, error)
}
