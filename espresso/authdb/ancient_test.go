package authdb

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/rawdb/ancienttest"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"

	"github.com/offchainlabs/nitro/cmd/util/integrityattestation"
)

var whitelistedKinds = []string{
	rawdb.ChainFreezerHashTable,
	rawdb.ChainFreezerHeaderTable,
	rawdb.ChainFreezerBodiesTable,
	rawdb.ChainFreezerReceiptTable,
}

// testAuthDB wraps AuthDB to dynamically map arbitrary test kinds to whitelisted kinds.
// This is necessary since ancienttest.TestAncientSuite will call with kind = "a", "b", etc.
type testAuthDB struct {
	authdb  *AuthDB
	kindMap map[string]string
}

func newTestAuthDB(authdb *AuthDB) *testAuthDB {
	t := &testAuthDB{
		authdb:  authdb,
		kindMap: make(map[string]string),
	}
	return t
}

// Returns the mapped kind for a test kind, adding it to the map if necessary
func (t *testAuthDB) mapKind(testKind string) (string, bool) {
	mapped, exists := t.kindMap[testKind]
	if exists {
		return mapped, true
	} else {
		if len(t.kindMap) >= len(whitelistedKinds) {
			return "", false
		}

		t.kindMap[testKind] = whitelistedKinds[len(t.kindMap)]
		return t.kindMap[testKind], true
	}
}

func (d *testAuthDB) AncientDatadir() (string, error) {
	return d.authdb.AncientDatadir()
}

func (t *testAuthDB) HasAncient(kind string, number uint64) (bool, error) {
	mapped, ok := t.mapKind(kind)
	if !ok {
		return false, nil
	}
	return t.authdb.HasAncient(mapped, number)
}

func (t *testAuthDB) Ancient(kind string, number uint64) ([]byte, error) {
	mapped, ok := t.mapKind(kind)
	if !ok {
		return nil, fmt.Errorf("unknown table %s", kind)
	}
	return t.authdb.Ancient(mapped, number)
}

func (t *testAuthDB) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	mapped, ok := t.mapKind(kind)
	if !ok {
		return nil, fmt.Errorf("unknown table %s", kind)
	}
	return t.authdb.AncientRange(mapped, start, count, maxBytes)
}

func (t *testAuthDB) AncientSize(kind string) (uint64, error) {
	mapped, ok := t.mapKind(kind)
	if !ok {
		return 0, fmt.Errorf("unknown table %s", kind)
	}
	return t.authdb.AncientSize(mapped)
}

func (t *testAuthDB) Ancients() (uint64, error) {
	return t.authdb.Ancients()
}

func (t *testAuthDB) Tail() (uint64, error) {
	return t.authdb.Tail()
}

func (t *testAuthDB) Sync() error {
	return t.authdb.Sync()
}

func (t *testAuthDB) TruncateHead(n uint64) (uint64, error) {
	return t.authdb.TruncateHead(n)
}

func (t *testAuthDB) TruncateTail(n uint64) (uint64, error) {
	return t.authdb.TruncateTail(n)
}

func (t *testAuthDB) Close() error {
	return t.authdb.Close()
}

func (t *testAuthDB) ReadAncients(fn func(ethdb.AncientReaderOp) error) error {
	return t.authdb.ReadAncients(func(op ethdb.AncientReaderOp) error {
		return fn(&testAncientReaderOp{inner: op, testdb: t})
	})
}

func (t *testAuthDB) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (int64, error) {
	return t.authdb.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		return fn(&testAncientWriteOp{inner: op, testdb: t})
	})
}

type testAncientReaderOp struct {
	inner  ethdb.AncientReaderOp
	testdb *testAuthDB
}

func (op *testAncientReaderOp) HasAncient(kind string, number uint64) (bool, error) {
	mapped, ok := op.testdb.mapKind(kind)
	if !ok {
		return false, nil
	}
	return op.inner.HasAncient(mapped, number)
}

func (op *testAncientReaderOp) Ancient(kind string, number uint64) ([]byte, error) {
	mapped, ok := op.testdb.mapKind(kind)
	if !ok {
		return nil, fmt.Errorf("unknown table %s", kind)
	}
	return op.inner.Ancient(mapped, number)
}

func (op *testAncientReaderOp) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	mapped, ok := op.testdb.mapKind(kind)
	if !ok {
		return nil, fmt.Errorf("unknown table %s", kind)
	}
	return op.inner.AncientRange(mapped, start, count, maxBytes)
}

func (op *testAncientReaderOp) Ancients() (uint64, error) {
	return op.inner.Ancients()
}

func (op *testAncientReaderOp) Tail() (uint64, error) {
	return op.inner.Tail()
}

func (op *testAncientReaderOp) AncientSize(kind string) (uint64, error) {
	mapped, ok := op.testdb.mapKind(kind)
	if !ok {
		return 0, fmt.Errorf("unknown table %s", kind)
	}
	return op.inner.AncientSize(mapped)
}

type testAncientWriteOp struct {
	inner  ethdb.AncientWriteOp
	testdb *testAuthDB
}

func (op *testAncientWriteOp) Append(kind string, number uint64, item interface{}) error {
	mapped, ok := op.testdb.mapKind(kind)
	if !ok {
		return fmt.Errorf("unknown table %s", kind)
	}
	return op.inner.Append(mapped, number, item)
}

func (op *testAncientWriteOp) AppendRaw(kind string, number uint64, item []byte) error {
	mapped, ok := op.testdb.mapKind(kind)
	if !ok {
		return fmt.Errorf("unknown table %s", kind)
	}
	return op.inner.AppendRaw(mapped, number, item)
}

// testDatabase wraps KeyValueStore and Freezer for test purposes
type testDatabase struct {
	ethdb.KeyValueStore
	*rawdb.Freezer
}

func (db *testDatabase) Close() error {
	if err := db.Freezer.Close(); err != nil {
		return err
	}
	return db.KeyValueStore.Close()
}

func (db *testDatabase) Compact(start []byte, limit []byte) error    { return nil }
func (db *testDatabase) Stat() (string, error)                       { return "", nil }
func (db *testDatabase) WasmDataBase() (ethdb.KeyValueStore, uint32) { return db.KeyValueStore, 0 }
func (db *testDatabase) WasmTargets() []ethdb.WasmTarget             { return nil }

// TestAuthDBAncientSuite runs geth's comprehensive ancient store test suite
// against AuthDB. This validates that all ancient store operations work correctly
// with authentication wrapping.
func TestAuthDBAncientSuite(t *testing.T) {
	ancienttest.TestAncientSuite(t, func(kinds []string) ethdb.AncientStore {
		if len(kinds) > len(whitelistedKinds) {
			t.Fatalf("only support max %d kinds", len(whitelistedKinds))
		}

		// Create only the tables we need for the test kinds
		tables := make(map[string]bool)
		for i := range len(kinds) {
			tables[whitelistedKinds[i]] = true
		}

		freezerDir := t.TempDir()
		freezer, err := rawdb.NewFreezer(freezerDir, "", false, 2049, tables)
		Require(t, err)
		db := &testDatabase{KeyValueStore: memorydb.New(), Freezer: freezer}

		mac, err := integrityattestation.GenerateHMAC()
		Require(t, err)
		// Create AuthDB with only the tag tables we need
		// NOTE: our NewAuthDB will default to a tagFreezer with all 4 tables whose internal sync
		// logic will ensure all tables have the same length. For our test, we only use <=4 tables
		// thus we use an internal constructor with specified freezer table
		// Convert main table names to tag table names
		tagTables := make(map[string]bool)
		for mainTable := range tables {
			if tagTable, ok := getTagTable(mainTable); ok {
				tagTables[tagTable] = true // No snappy compression for tags
			}
		}
		authDB, err := newAuthDBWithFreezerTables(db, mac, tagTables)
		Require(t, err)

		return newTestAuthDB(&authDB)
	})
}

// TestAuthDBAncientSuiteNoAuth runs the test suite without authentication enabled.
// This validates that AuthDB correctly passes through to the underlying store
// when mac is nil.
func TestAuthDBAncientSuiteNoAuth(t *testing.T) {
	ancienttest.TestAncientSuite(t, func(kinds []string) ethdb.AncientStore {
		tables := make(map[string]bool)
		for _, kind := range kinds {
			tables[kind] = true
		}

		freezerDir := t.TempDir()
		freezer, err := rawdb.NewFreezer(freezerDir, "", false, 2049, tables)
		Require(t, err)
		db := &testDatabase{KeyValueStore: memorydb.New(), Freezer: freezer}

		authDB, err := NewAuthDB(db, nil)
		Require(t, err)
		return &authDB
	})
}
