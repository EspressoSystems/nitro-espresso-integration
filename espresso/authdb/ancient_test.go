package authdb

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/rawdb/ancienttest"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"

	espresso_tee_utils "github.com/offchainlabs/nitro/cmd/util/espresso-tee-utils"
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
	*AuthDB // Embedded - inherits Ancients, Tail, Sync, TruncateHead, TruncateTail, Close, AncientDatadir
	kindMap map[string]string
}

func newTestAuthDB(authdb *AuthDB) *testAuthDB {
	t := &testAuthDB{
		AuthDB:  authdb,
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

func (t *testAuthDB) HasAncient(kind string, number uint64) (bool, error) {
	mapped, ok := t.mapKind(kind)
	if !ok {
		return false, nil
	}
	return t.AuthDB.HasAncient(mapped, number)
}

func (t *testAuthDB) Ancient(kind string, number uint64) ([]byte, error) {
	mapped, ok := t.mapKind(kind)
	if !ok {
		return nil, fmt.Errorf("unknown table %s", kind)
	}
	return t.AuthDB.Ancient(mapped, number)
}

func (t *testAuthDB) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	mapped, ok := t.mapKind(kind)
	if !ok {
		return nil, fmt.Errorf("unknown table %s", kind)
	}
	return t.AuthDB.AncientRange(mapped, start, count, maxBytes)
}

func (t *testAuthDB) AncientSize(kind string) (uint64, error) {
	mapped, ok := t.mapKind(kind)
	if !ok {
		return 0, fmt.Errorf("unknown table %s", kind)
	}
	return t.AuthDB.AncientSize(mapped)
}

func (t *testAuthDB) ReadAncients(fn func(ethdb.AncientReaderOp) error) error {
	return t.AuthDB.ReadAncients(func(op ethdb.AncientReaderOp) error {
		return fn(&testAncientReaderOp{AncientReaderOp: op, testdb: t})
	})
}

func (t *testAuthDB) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (int64, error) {
	return t.AuthDB.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		return fn(&testAncientWriteOp{AncientWriteOp: op, testdb: t})
	})
}

type testAncientReaderOp struct {
	ethdb.AncientReaderOp // Embedded - inherits Ancients, Tail
	testdb                *testAuthDB
}

func (op *testAncientReaderOp) HasAncient(kind string, number uint64) (bool, error) {
	mapped, ok := op.testdb.mapKind(kind)
	if !ok {
		return false, nil
	}
	return op.AncientReaderOp.HasAncient(mapped, number)
}

func (op *testAncientReaderOp) Ancient(kind string, number uint64) ([]byte, error) {
	mapped, ok := op.testdb.mapKind(kind)
	if !ok {
		return nil, fmt.Errorf("unknown table %s", kind)
	}
	return op.AncientReaderOp.Ancient(mapped, number)
}

func (op *testAncientReaderOp) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	mapped, ok := op.testdb.mapKind(kind)
	if !ok {
		return nil, fmt.Errorf("unknown table %s", kind)
	}
	return op.AncientReaderOp.AncientRange(mapped, start, count, maxBytes)
}

func (op *testAncientReaderOp) AncientSize(kind string) (uint64, error) {
	mapped, ok := op.testdb.mapKind(kind)
	if !ok {
		return 0, fmt.Errorf("unknown table %s", kind)
	}
	return op.AncientReaderOp.AncientSize(mapped)
}

type testAncientWriteOp struct {
	ethdb.AncientWriteOp // Embedded - no pure pass-through methods, but cleaner interface
	testdb               *testAuthDB
}

func (op *testAncientWriteOp) Append(kind string, number uint64, item interface{}) error {
	mapped, ok := op.testdb.mapKind(kind)
	if !ok {
		return fmt.Errorf("unknown table %s", kind)
	}
	return op.AncientWriteOp.Append(mapped, number, item)
}

func (op *testAncientWriteOp) AppendRaw(kind string, number uint64, item []byte) error {
	mapped, ok := op.testdb.mapKind(kind)
	if !ok {
		return fmt.Errorf("unknown table %s", kind)
	}
	return op.AncientWriteOp.AppendRaw(mapped, number, item)
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

		mac, err := espresso_tee_utils.HmacForTest()
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
		authDB, err := newAuthDBWithFreezerTables(db, mac, tagTables, false)
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

		authDB, err := NewAuthDB(db, nil, false)
		Require(t, err)
		return &authDB
	})
}
