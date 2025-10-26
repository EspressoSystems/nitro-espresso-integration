package arbnode

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/espresso/authdb"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type EspressoSnapshotHandler struct {
	stopwaiter.StopWaiter
	db               *authdb.AuthDB
	parentChainDir   string
	generateSnapshot bool
}

func NewEspressoSnapshotHandler(db *authdb.AuthDB, parentChainDir string, generateSnapshot bool) *EspressoSnapshotHandler {
	return &EspressoSnapshotHandler{
		db:               db,
		parentChainDir:   parentChainDir,
		generateSnapshot: generateSnapshot,
	}
}

func (s *EspressoSnapshotHandler) StoreSnapshotSha256(sum string) error {
	file, err := os.Create(filepath.Join(s.parentChainDir, "snapshot.txt"))
	if err != nil {
		return fmt.Errorf("failed to create snapshot file: %w", err)
	}
	defer file.Close()
	_, err = file.WriteString(sum + "\n")
	if err != nil {
		return fmt.Errorf("failed to write snapshot file: %w", err)
	}
	log.Info("Stored the snapshot hash in a file", "sha256_h1", sum)
	return nil
}

func (s *EspressoSnapshotHandler) Start(ctx context.Context) error {
	s.StopWaiter.Start(ctx, s)
	err := s.db.InitAuthTagsDatabase()
	if err != nil {
		return fmt.Errorf("failed to verify snapshot: %w", err)
	}

	return nil
}

func (s *EspressoSnapshotHandler) CreateAndSnapshot() error {
	parentDir := filepath.Dir(s.parentChainDir)
	l2chainDataPath := path.Join(parentDir, "l2chaindata")

	sha256Hash, err := arbutil.HashDirectory(l2chainDataPath)
	if err != nil {
		return err
	}
	log.Info("Hashed the l2chaindata dir", "sha256_h1", sha256Hash)
	// Store the sha256 hash of the snapshot
	err = s.StoreSnapshotSha256(sha256Hash)
	if err != nil {
		return err
	}
	return nil
}

func (s *EspressoSnapshotHandler) StopAndWait() {
	s.StopWaiter.StopAndWait()
	// Only generate snapshot if generateSnapshot is true
	if s.generateSnapshot {
		s.db.Close()
		log.Info("Taking snapshot of the database, this may take a while")
		err := s.CreateAndSnapshot()
		if err != nil {
			log.Error("Failed to create snapshot", "err", err)
			return
		}
		log.Info("Snapshot taken and stored")
	}
}
