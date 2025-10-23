package authdb

import (
	"path/filepath"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/log"
)

// newAuthTagFreezer creates a freezer instance dedicated to storing authentication tags.
// The tag freezer is stored in a separate directory from the main chain freezer and
// contains one table per main ancient table (hashes, headers, bodies, receipts).
//
// Parameters:
//   - ancientDir: root ancient directory (empty string for in-memory mode)
//   - readonly: whether to open in read-only mode
//   - tables: map of table names to booleans indicating whether snappy compression is disabled
//
// Returns nil freezer (without error) if ancientDir is empty, allowing in-memory operation.
func newAuthTagFreezerWithTables(ancientDir string, readonly bool, tables map[string]bool) (*rawdb.Freezer, error) {
	if ancientDir == "" {
		return nil, nil
	}

	// Construct tag freezer path
	tagFreezerDir := filepath.Join(ancientDir, AuthTagFreezerName)

	// Create the tag freezer with appropriate table configuration
	// Use table size (100MB) much smaller than chain freezer (2GB) and disable snappy compression
	// since HMAC tags are random data that doesn't compress well
	const tagFreezerTableSize = 100 * 1000 * 1000
	tagFreezer, err := rawdb.NewFreezer(
		tagFreezerDir,
		"authdb/tags",
		readonly,
		tagFreezerTableSize,
		tables,
	)
	if err != nil {
		return nil, err
	}

	log.Info("Opened authentication tag freezer", "path", tagFreezerDir, "readonly", readonly)
	return tagFreezer, nil
}
