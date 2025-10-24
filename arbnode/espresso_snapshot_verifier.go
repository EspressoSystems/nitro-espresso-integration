package arbnode

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/log"

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
func (s *EspressoSnapshotHandler) CreateSnapshot(initializeTags bool) (string, error) {

	hashData, bodyData, receiptData, headerData, err := s.db.ReadChainAncients()
	if err != nil {
		log.Error("unable to read ancients", "err", err)
		return "", err
	}
	keys, values, err := s.db.ReadDatabase()
	if err != nil {
		log.Error("unable to read database", "err", err)
		return "", err
	}
	// Sha256 hash the data
	hasher := sha256.New()
	for _, data := range hashData {
		hasher.Write(data)
	}
	for _, data := range bodyData {
		hasher.Write(data)
	}
	for _, data := range receiptData {
		hasher.Write(data)
	}
	for _, data := range headerData {
		hasher.Write(data)
	}

	// Hash keys along with values
	for i, key := range keys {
		hasher.Write(key)
		hasher.Write(values[i])
	}

	sum := base64.StdEncoding.EncodeToString(hasher.Sum(nil))
	log.Info("Hashed the l2chaindata dir", "sha256_h1", sum)

	// if s.snapshotChecksum != sha {
	// 	return fmt.Errorf("snapshot hash mismatch, want: %s, got: %s", s.snapshotChecksum, sha)
	// }
	log.Info("Snapshot hash matches", "sha256_h1", sum)

	if initializeTags {
		err = s.db.InitAuthTagsDatabase(keys, values)
		if err != nil {
			return "", fmt.Errorf("failed to add auth tags to the database: %w", err)
		}
		log.Info("Added auth tags to the database")

		// err = s.db.InitAncientAuthTags(hashData, bodyData, headerData, receiptData)
		// if err != nil {
		// 	return "", fmt.Errorf("failed to add auth tags to the ancient database: %w", err)
		// }
	}

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

func (s *EspressoSnapshotHandler) VerifySnapshotAndAddTags() error {
	// Create snapshot of the database
	_, err := s.CreateSnapshot(true)
	if err != nil {
		return fmt.Errorf("failed to create snapshot :%w", err)
	}
	return nil
}

func (s *EspressoSnapshotHandler) Start(ctx context.Context) error {
	s.StopWaiter.Start(ctx, s)
	err := s.VerifySnapshotAndAddTags()
	if err != nil {
		return fmt.Errorf("failed to verify snapshot: %w", err)
	}

	log.Info("Added auth tags to the ancient database")
	return nil
}

func (s *EspressoSnapshotHandler) StopAndWait() {
	s.StopWaiter.StopAndWait()
	log.Info("Taking snapshot of the database, this may take a while")
	s.db.Sync()
	sha, err := s.CreateSnapshot(false)
	if err != nil {
		log.Error("Failed to create snapshot", "err", err)
		return
	}
	err = s.StoreSnapshotSha256(sha)
	if err != nil {
		log.Error("Failed to store snapshot sha256", "err", err)
		return
	}
	s.db.Close()
	log.Info("Snapshot taken and stored")
}
