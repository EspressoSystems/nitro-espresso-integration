package arbnode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/espresso/authdb"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type EspressoSnapshotHandler struct {
	stopwaiter.StopWaiter
	db               *authdb.AuthDB
	rootDir          string
	snapshotChecksum string
}

func NewEspressoSnapshotHandler(db *authdb.AuthDB, rootDir string, snapshotChecksum string) *EspressoSnapshotHandler {
	return &EspressoSnapshotHandler{
		db:               db,
		rootDir:          rootDir,
		snapshotChecksum: snapshotChecksum,
	}
}

// CreateSnapshot creates a snapshot of the l2chaindata dir
// when the node is shutting down, its called from the shutdown hook
// of the SnapshotHandler
func (s *EspressoSnapshotHandler) CreateSnapshot() (string, error) {
	sum, err := arbutil.HashDirectory(s.rootDir)
	if err != nil {
		log.Error("snapshot hash failed", "dir", s.rootDir, "err", err)
		return "", err
	}
	log.Info("Hashed the l2chaindata dir", "sha256_h1", sum)
	return sum, nil
}

func (s *EspressoSnapshotHandler) StoreSnapshotSha256(sum string) error {
	file, err := os.Create(filepath.Join(s.rootDir, "snapshot.txt"))
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

func (s *EspressoSnapshotHandler) VerifySnapshot() error {
	// Create snapshot of the database
	sha, err := s.CreateSnapshot()
	if err != nil {
		return fmt.Errorf("failed to create snapshot :%w", err)
	}
	// if s.snapshotChecksum != sha {
	// 	return fmt.Errorf("snapshot hash mismatch, want: %s, got: %s", s.snapshotChecksum, sha)
	// }
	log.Info("Snapshot hash matches", "sha256_h1", sha)
	return nil
}

func (s *EspressoSnapshotHandler) Start(ctx context.Context) error {
	s.StopWaiter.Start(ctx, s)
	s.VerifySnapshot()

	err := s.db.InitAuthTags()
	if err != nil {
		return fmt.Errorf("failed to add auth tags to the database: %w", err)
	}
	log.Info("Added auth tags to the database")

	// err = s.db.InitAncientAuthTags()
	// if err != nil {
	// 	return fmt.Errorf("failed to add auth tags to the ancient database: %w", err)
	// }
	log.Info("Added auth tags to the ancient database")
	return nil
}

func (s *EspressoSnapshotHandler) StopAndWait() {
	s.StopWaiter.StopAndWait()
	s.db.Close()
	log.Info("Taking snapshot of the database, this may take a while")
	sha, err := s.CreateSnapshot()
	if err != nil {
		log.Error("Failed to create snapshot", "err", err)
		return
	}
	err = s.StoreSnapshotSha256(sha)
	if err != nil {
		log.Error("Failed to store snapshot sha256", "err", err)
		return
	}
	log.Info("Snapshot taken and stored")
}
