package arbnode

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/espresso/authdb"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type EspressoSnapshotHandler struct {
	stopwaiter.StopWaiter
	db     authdb.AuthDB
	hasher crypto.KeccakState
}

func NewEspressoSnapshotHandler(db authdb.AuthDB) *EspressoSnapshotHandler {
	hasher := crypto.NewKeccakState()
	return &EspressoSnapshotHandler{
		db:     db,
		hasher: hasher,
	}
}

func (s *EspressoSnapshotHandler) Start(ctx context.Context) error {
	s.StopWaiter.Start(ctx, s)
	err := s.db.InitAuthTags()
	if err != nil {
		return fmt.Errorf("failed to add auth tags to the database: %w", err)
	}
	log.Info("Added auth tags to the database")

	err = s.db.InitAncientAuthTags()
	if err != nil {
		return fmt.Errorf("failed to add auth tags to the ancient database: %w", err)
	}
	log.Info("Added auth tags to the ancient database")
	return nil
}
