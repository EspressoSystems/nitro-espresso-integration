package arbnode

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/offchainlabs/nitro/espresso/authdb"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type EspressoSnapshotVerifier struct {
	stopwaiter.StopWaiter
	snapshotHash common.Hash
	db           authdb.AuthDB
	hasher       crypto.KeccakState
}

func NewEspressoSnapshotVerifier(snapshotHash common.Hash, db authdb.AuthDB) *EspressoSnapshotVerifier {
	hasher := crypto.NewKeccakState()
	return &EspressoSnapshotVerifier{
		snapshotHash: snapshotHash,
		db:           db,
		hasher:       hasher,
	}
}

// AppendAuthTags adds auth tags to all key value pairs in the database

func (s *EspressoSnapshotVerifier) VerifySnapshot(ctx context.Context) error {

	return nil
}

func (s *EspressoSnapshotVerifier) Start(ctx context.Context) error {
	s.StopWaiter.Start(ctx, s)

	err := s.VerifySnapshot(ctx)
	if err != nil {
		return fmt.Errorf("failed to verify snapshot: %w", err)
	}

	err = s.db.InitAuthTags()
	if err != nil {
		return fmt.Errorf("failed to add auth tags to the database: %w", err)
	}

	return nil
}
