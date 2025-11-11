package arbnode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/espresso/authdb"
	espresso_key_manager "github.com/offchainlabs/nitro/espresso/key-manager"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type EspressoSnapshotHandler struct {
	stopwaiter.StopWaiter
	db               *authdb.AuthDB
	parentChainDir   string
	l2chainDataDir   string
	initializeTags   bool
	generateSnapshot bool
	keyManager       *espresso_key_manager.EspressoKeyManager
}

func NewEspressoSnapshotHandler(db *authdb.AuthDB, parentChainDir string, l2chainDataDir string, initializeTags bool, keyManager *espresso_key_manager.EspressoKeyManager, generateSnapshot bool) *EspressoSnapshotHandler {
	return &EspressoSnapshotHandler{
		db:               db,
		parentChainDir:   parentChainDir,
		l2chainDataDir:   l2chainDataDir,
		initializeTags:   initializeTags,
		keyManager:       keyManager,
		generateSnapshot: generateSnapshot,
	}
}

func (s *EspressoSnapshotHandler) StoreSnapshotSha256(sum string) error {
	path := filepath.Join(s.parentChainDir, "snapshot.txt")
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create snapshot file: %w", err)
	}
	defer file.Close()
	_, err = file.WriteString(sum)
	if err != nil {
		return fmt.Errorf("failed to write snapshot file: %w", err)
	}
	log.Info("Stored the snapshot hash in a file", "sha256_h1", sum, "path", path)
	return nil
}

func (s *EspressoSnapshotHandler) StoreSnapshotSignature(signature []byte) error {
	path := filepath.Join(s.parentChainDir, "snapshot_signature.txt")
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create snapshot file: %w", err)
	}
	defer file.Close()
	_, err = file.Write(signature)
	if err != nil {
		return fmt.Errorf("failed to write snapshot file: %w", err)
	}
	log.Info("Stored the snapshot signature in a file", "path", path)
	return nil
}

func (s *EspressoSnapshotHandler) Start(ctx context.Context) error {
	s.StopWaiter.Start(ctx, s)
	if !s.initializeTags {
		log.Warn("Initialization of auth tags is disabled, skipping")
		return nil
	}

	// Only if snapshot mode is enabled, we re-initialize the tags
	err := s.db.InitAuthTagsDatabase()
	if err != nil {
		return fmt.Errorf("failed to add auth tags to the database: %w", err)
	}
	err = s.db.InitAncientAuthTags()
	if err != nil {
		return fmt.Errorf("failed to add ancient auth tags: %w", err)
	}

	return nil
}

func (s *EspressoSnapshotHandler) CreateAndSnapshot() error {
	// Close the database before creating the snapshot
	s.db.Close()

	sha256Hash, err := arbutil.HashDir(s.l2chainDataDir)
	if err != nil {
		return err
	}
	log.Info("Hashed the l2chaindata dir", "sha256_h1", sha256Hash)
	// Store the sha256 hash of the snapshot
	err = s.StoreSnapshotSha256(sha256Hash)
	if err != nil {
		return err
	}
	if s.keyManager != nil {
		signedBytes, err := s.keyManager.SignMessage([]byte(sha256Hash))
		if err != nil {
			return fmt.Errorf("failed to sign the snapshot hash: %w", err)
		}
		err = s.StoreSnapshotSignature(signedBytes)
		if err != nil {
			return fmt.Errorf("failed to store snapshot signature: %w", err)
		}
		log.Info("Signed the snapshot hash using the key manager")
	}

	return nil
}

func (s *EspressoSnapshotHandler) StopAndWait() {
	s.StopWaiter.StopAndWait()
	if !s.generateSnapshot {
		log.Info("Snapshot generation is disabled, skipping")
		return
	}
	log.Info("Taking snapshot of the database, this may take a while")
	err := s.CreateAndSnapshot()
	if err != nil {
		log.Error("Failed to create snapshot", "err", err)
	}
	log.Info("Snapshot taken and stored")
}
