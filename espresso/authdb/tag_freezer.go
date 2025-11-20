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

	tagFreezerDir := filepath.Join(ancientDir, AuthTagFreezerName)
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
