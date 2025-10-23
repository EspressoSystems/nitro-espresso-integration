package authdb

import (
	"bytes"
	"crypto/hmac"
	"errors"
	"fmt"
	"hash"
	"math"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/util/dbutil"
)

var (
	// ErrAuthTagMissing is returned when a tag cannot be found in the tag store
	ErrAuthTagMissing = errors.New("authentication tag missing")
	// ErrAuthTagMismatch is returned when a tag doesn't match the computed value
	ErrAuthTagMismatch = errors.New("authentication tag mismatch")
)

type AuthDB struct {
	db  ethdb.Database
	mac hash.Hash // HMAC func or nil to disable authentication

	// Tag freezer stores authentication tags in separate ancient store
	tagFreezer *rawdb.Freezer
}

func NewAuthDB(db ethdb.Database, mac hash.Hash) (AuthDB, error) {
	return newAuthDBWithFreezerTables(db, mac, authTagTableNoSnappy)
}

func newAuthDBWithFreezerTables(db ethdb.Database, mac hash.Hash, tagTables map[string]bool) (AuthDB, error) {
	if db == nil {
		return AuthDB{}, errors.New("db is nil during authdb creation")
	}

	authDB := AuthDB{
		db:  db,
		mac: mac,
	}

	if mac == nil {
		log.Warn("new AuthDB with authentication disabled")
		return authDB, nil
	}

	// Determine ancient directory
	ancientDir, err := db.AncientDatadir()
	if err != nil || ancientDir == "" {
		log.Warn("no/empty ancient datadir: skip authenticating ancient store")
		return authDB, nil //nolint:nilerr
	}

	// Initialize tag freezer
	// Note: We don't know if the DB is read-only from this interface
	// The freezer will handle file locks appropriately
	tagFreezer, err := newAuthTagFreezerWithTables(ancientDir, false, tagTables)
	if err != nil {
		return authDB, fmt.Errorf("failed to initialize tag freezer: %w", err)
	}
	authDB.tagFreezer = tagFreezer

	log.Info("AuthDB initialized with tag freezer", "ancient_dir", ancientDir)
	return authDB, nil
}

// computeMac computes HMAC for key-value pair
func (d *AuthDB) computeMac(key []byte, val []byte) []byte {
	d.mac.Reset()
	d.mac.Write(key)
	d.mac.Write(val)
	return d.mac.Sum(nil)
}

// either in-memory check (e.g. Freezer) or delegated to `Ancient` (e.g. remotedb.Database),
// thus safe to pass through
func (d *AuthDB) HasAncient(kind string, number uint64) (bool, error) {
	return d.db.HasAncient(kind, number)
}

func (d *AuthDB) Ancient(kind string, number uint64) ([]byte, error) {
	data, err := d.db.Ancient(kind, number)
	if err != nil {
		return nil, err
	}

	if d.mac == nil {
		return data, nil
	}

	return d.verifyAncientTag(kind, number, data)
}

// computeMacForAncient computes HMAC for kind, number, and data
func (d *AuthDB) computeMacForAncient(kind string, number uint64, data []byte) []byte {
	d.mac.Reset()
	d.mac.Write([]byte(kind))
	d.mac.Write(EncodeUint64(number))
	d.mac.Write(data)
	return d.mac.Sum(nil)
}

// verifyAncientTag reads the tag from the tag store and verifies it matches the data
func (d *AuthDB) verifyAncientTag(kind string, number uint64, data []byte) ([]byte, error) {
	// Get the corresponding tag table name
	tagTable, ok := getTagTable(kind)
	if !ok {
		return nil, fmt.Errorf("%w: kind: %s", errors.ErrUnsupported, kind)
	}

	// Retrieve stored tag from tag freezer
	if d.tagFreezer == nil {
		return nil, errors.New("tag freezer not initialized")
	}

	storedTag, err := d.tagFreezer.Ancient(tagTable, number)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAuthTagMissing, err)
	}

	// Compute expected tag
	expectedTag := d.computeMacForAncient(kind, number, data)

	// Verify tag
	if !hmac.Equal(expectedTag, storedTag) {
		return nil, fmt.Errorf("%w: kind=%s number=%d", ErrAuthTagMismatch, kind, number)
	}

	return data, nil
}

func (d *AuthDB) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	raw, err := d.db.AncientRange(kind, start, count, maxBytes)
	if err != nil {
		return nil, err
	}

	if d.mac == nil {
		return raw, nil
	}

	// Verify authentication for each item
	result := make([][]byte, len(raw))
	// overflow check
	if start > math.MaxUint64-uint64(len(raw)) {
		return nil, fmt.Errorf("ancient range uint64 overflow: start %d, len %d", start, len(raw))
	}

	for i, data := range raw {
		number := start + uint64(i) // #nosec G115 - i is bounded by len(raw)
		verifiedData, err := d.verifyAncientTag(kind, number, data)
		if err != nil {
			return nil, err
		}
		result[i] = verifiedData
	}

	return result, nil
}

// unauthenticated, but not security-sensitive
func (d *AuthDB) Ancients() (uint64, error) {
	return d.db.Ancients()
}

// unauthenticated, but not security-sensitive
func (d *AuthDB) Tail() (uint64, error) {
	return d.db.Tail()
}

// we don't change ancient size during `ModifyAncients`.
// unauthenticated, but not security-sensitive
func (d *AuthDB) AncientSize(kind string) (uint64, error) {
	return d.db.AncientSize(kind)
}

func (d *AuthDB) ReadAncients(fn func(ethdb.AncientReaderOp) error) error {
	return d.db.ReadAncients(func(op ethdb.AncientReaderOp) error {
		return fn(&AuthAncientReaderOp{inner: op, authDB: d})
	})
}

// AuthAncientReaderOp wraps AncientReaderOp to provide authenticated reads
type AuthAncientReaderOp struct {
	inner  ethdb.AncientReaderOp
	authDB *AuthDB
}

func (op *AuthAncientReaderOp) HasAncient(kind string, number uint64) (bool, error) {
	return op.authDB.HasAncient(kind, number)
}

func (op *AuthAncientReaderOp) Ancient(kind string, number uint64) ([]byte, error) {
	return op.authDB.Ancient(kind, number)
}

func (op *AuthAncientReaderOp) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	return op.authDB.AncientRange(kind, start, count, maxBytes)
}

func (op *AuthAncientReaderOp) Ancients() (uint64, error) {
	return op.authDB.Ancients()
}

func (op *AuthAncientReaderOp) Tail() (uint64, error) {
	return op.authDB.Tail()
}

func (op *AuthAncientReaderOp) AncientSize(kind string) (uint64, error) {
	return op.authDB.AncientSize(kind)
}

// AuthAncientWriteOp wraps AncientWriteOp to write data and tags separately
type AuthAncientWriteOp struct {
	inner    ethdb.AncientWriteOp
	authDB   *AuthDB
	tagBatch []tagWrite // Collect tag writes to apply after main writes
}

type tagWrite struct {
	kind   string
	number uint64
	tag    []byte
}

// Append writes structured data to main store and computes tag
func (op *AuthAncientWriteOp) Append(kind string, number uint64, item interface{}) error {
	// Write original data unchanged to main store
	if err := op.inner.Append(kind, number, item); err != nil {
		return err
	}

	// If authentication disabled, we're done
	if op.authDB.mac == nil {
		return nil
	}

	// Compute tag over RLP-encoded item
	var buf bytes.Buffer
	if err := rlp.Encode(&buf, item); err != nil {
		return fmt.Errorf("failed to RLP encode for tag: %w", err)
	}

	tag := op.authDB.computeMacForAncient(kind, number, buf.Bytes())

	// Store tag write for later
	op.tagBatch = append(op.tagBatch, tagWrite{
		kind:   kind,
		number: number,
		tag:    tag,
	})

	return nil
}

// AppendRaw writes raw data to main store and computes tag
func (op *AuthAncientWriteOp) AppendRaw(kind string, number uint64, item []byte) error {
	// Write original data unchanged to main store
	if err := op.inner.AppendRaw(kind, number, item); err != nil {
		return err
	}

	// If authentication disabled, we're done
	if op.authDB.mac == nil {
		return nil
	}

	// Compute tag over raw bytes
	tag := op.authDB.computeMacForAncient(kind, number, item)

	// Store tag write for later
	op.tagBatch = append(op.tagBatch, tagWrite{
		kind:   kind,
		number: number,
		tag:    tag,
	})

	return nil
}

// writeTags writes all collected tags to the tag store
func (op *AuthAncientWriteOp) writeTags() error {
	if op.authDB.mac == nil || len(op.tagBatch) == 0 {
		return nil
	}

	if op.authDB.tagFreezer == nil {
		return errors.New("tag freezer not initialized")
	}

	// Write to tag freezer
	_, err := op.authDB.tagFreezer.ModifyAncients(func(tagOp ethdb.AncientWriteOp) error {
		for _, tw := range op.tagBatch {
			tagTable, ok := getTagTable(tw.kind)
			if !ok {
				continue // Skip unsupported tables
			}
			if err := tagOp.AppendRaw(tagTable, tw.number, tw.tag); err != nil {
				return fmt.Errorf("failed to write tag for %s#%d: %w", tw.kind, tw.number, err)
			}
		}
		return nil
	})
	return err
}

func (d *AuthDB) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (int64, error) {
	if d.mac == nil {
		return d.db.ModifyAncients(fn)
	}

	// Create our wrapper that collects tag writes
	var authOp *AuthAncientWriteOp

	// Execute main writes
	writeSize, err := d.db.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		authOp = &AuthAncientWriteOp{
			inner:    op,
			authDB:   d,
			tagBatch: make([]tagWrite, 0),
		}
		return fn(authOp)
	})

	// Main write failed - don't write tags
	if err != nil {
		return writeSize, err
	}

	// Main writes succeeded - now write tags
	if authOp != nil {
		if tagErr := authOp.writeTags(); tagErr != nil {
			// Future reads will fail verification
			log.Crit("Failed to write authentication tags after successful main write",
				"error", tagErr, "writes", len(authOp.tagBatch))
			return writeSize, fmt.Errorf("tag write failed: %w", tagErr)
		}
	}

	return writeSize, nil
}

func (d *AuthDB) TruncateHead(n uint64) (uint64, error) {
	old, err := d.db.TruncateHead(n)
	if err != nil {
		return old, err
	}

	// Truncate tag freezer to match
	if d.mac != nil && d.tagFreezer != nil {
		// Truncate all tag tables to the same head, but only if they have data
		for _, tagTable := range freezerTabletoTagTable {
			// Check if table has any items before truncating
			if items, _ := d.tagFreezer.Ancients(); items > 0 {
				if _, tagErr := d.tagFreezer.TruncateHead(n); tagErr != nil {
					log.Error("Failed to truncate tag freezer head", "table", tagTable, "n", n, "err", tagErr)
				}
			}
		}
	}

	return old, nil
}

func (d *AuthDB) TruncateTail(n uint64) (uint64, error) {
	old, err := d.db.TruncateTail(n)
	if err != nil {
		return old, err
	}

	// Truncate tag freezer to match
	if d.mac != nil && d.tagFreezer != nil {
		// Truncate all tag tables to the same tail, but only if they have data
		for _, tagTable := range freezerTabletoTagTable {
			// Check if table has any items before truncating
			if items, _ := d.tagFreezer.Ancients(); items > 0 {
				if _, tagErr := d.tagFreezer.TruncateTail(n); tagErr != nil {
					log.Error("Failed to truncate tag freezer tail", "table", tagTable, "n", n, "err", tagErr)
				}
			}
		}
	}

	return old, nil
}

func (d *AuthDB) Sync() error {
	err := d.db.Sync()
	if err != nil {
		return err
	}

	// Sync tag freezer
	if d.tagFreezer != nil {
		if tagErr := d.tagFreezer.Sync(); tagErr != nil {
			return fmt.Errorf("failed to sync tag freezer: %w", tagErr)
		}
	}

	return nil
}

func (d *AuthDB) AncientDatadir() (string, error) {
	return d.db.AncientDatadir()
}

func (d *AuthDB) Stat() (string, error) {
	return d.db.Stat()
}

func (d *AuthDB) Close() error {
	var errs []error

	// Close tag freezer first
	if d.tagFreezer != nil {
		if err := d.tagFreezer.Close(); err != nil {
			log.Error("Failed to close tag freezer", "err", err)
			errs = append(errs, fmt.Errorf("tag freezer close: %w", err))
		}
	}

	// Close main DB
	if err := d.db.Close(); err != nil {
		errs = append(errs, fmt.Errorf("main db close: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

func (d *AuthDB) Compact(start []byte, limit []byte) error {
	return d.db.Compact(start, limit)
}

func (d *AuthDB) Delete(key []byte) error {
	return d.db.Delete(key)
}

func (d *AuthDB) DeleteRange(start []byte, end []byte) error {
	return d.db.DeleteRange(start, end)
}

func (d *AuthDB) NewBatch() ethdb.Batch {
	inner := d.db.NewBatch()
	return &AuthBatch{inner: inner, authDB: d}
}

func (d *AuthDB) NewBatchWithSize(size int) ethdb.Batch {
	inner := d.db.NewBatchWithSize(size)
	return &AuthBatch{inner: inner, authDB: d}
}

// Directly pass through because WasmDB is mostly used during fraud game in-memory simulation, nothing persistent
func (d *AuthDB) WasmDataBase() (ethdb.KeyValueStore, uint32) {
	return d.db.WasmDataBase()
}

func (d *AuthDB) WasmTargets() []ethdb.WasmTarget {
	return d.db.WasmTargets()
}

func (d *AuthDB) Put(key []byte, value []byte) error {
	// Always store the actual data first
	err := d.db.Put(key, value)
	if err != nil {
		return err
	}

	if d.mac == nil {
		return nil
	}

	// add auth tags to every entry
	d.mac.Reset()
	d.mac.Write(key)
	d.mac.Write(value)
	tag := d.mac.Sum(nil)

	err = d.db.Put(genericAuthTagKey(key), tag)
	if err != nil {
		log.Crit("failed to write auth tag", "dbkey", key, "err", err)
		return err
	}
	return nil
}

func (d *AuthDB) Has(key []byte) (bool, error) {
	_, err := d.Get(key)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to Get during Has: %w", err)
	}

	return true, nil
}

func (d *AuthDB) Get(key []byte) ([]byte, error) {
	val, err := d.db.Get(key)
	if err != nil {
		return nil, err
	}

	if d.mac == nil {
		return val, nil
	}

	expectedTag, err := d.db.Get(genericAuthTagKey(key))
	if err != nil {
		log.Error("Failed to get auth tag", "dbkey", key, "err", err)
		return nil, err
	}

	tag := d.computeMac(key, val)

	if !hmac.Equal(tag, expectedTag) {
		log.Error("failed to authenticate", "key", key, "val", val)
		return nil, fmt.Errorf("%w: key=%v val=%d", ErrAuthTagMismatch, key, val)
	}

	return val, nil
}

func (d *AuthDB) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	inner := d.db.NewIterator(prefix, start)
	it := NewAuthIterator(inner, d)
	return &it
}

type AuthIterator struct {
	inner ethdb.Iterator
	db    *AuthDB
}

func NewAuthIterator(inner ethdb.Iterator, db *AuthDB) AuthIterator {
	return AuthIterator{inner: inner, db: db}
}

func (it *AuthIterator) Next() bool {
	for it.inner.Next() {
		// Skip auth tag keys that end with "-tag"
		key := it.inner.Key()
		if !bytes.HasSuffix(key, genericAuthTagSuffix) {
			return true
		}
	}
	return false
}

func (it *AuthIterator) Error() error {
	return it.inner.Error()
}

func (it *AuthIterator) Key() []byte {
	return it.inner.Key()
}

func (it *AuthIterator) Value() []byte {
	key := it.Key()
	val := it.inner.Value()
	if it.db.mac == nil {
		return val
	}

	expectedTag, err := it.db.db.Get(genericAuthTagKey(key))
	if err != nil {
		log.Error("Failed to get auth tag", "dbkey", key, "err", err)
		return nil
	}

	tag := it.db.computeMac(key, val)

	if !hmac.Equal(tag, expectedTag) {
		log.Error("failed to authenticate", "key", key, "val", val)
		return nil
	}
	return val
}

func (it *AuthIterator) Release() {
	it.inner.Release()
}

// AuthBatch wraps ethdb.Batch to provide authenticated batch operations
type AuthBatch struct {
	inner  ethdb.Batch
	authDB *AuthDB
}

// Put adds a key-value pair to the batch
func (b *AuthBatch) Put(key []byte, value []byte) error {
	if err := b.inner.Put(key, value); err != nil {
		return err
	}

	if b.authDB.mac != nil {
		tag := b.authDB.computeMac(key, value)

		if err := b.inner.Put(genericAuthTagKey(key), tag); err != nil {
			return fmt.Errorf("failed to put auth tag for key %s: %w", key, err)
		}

	}
	return nil
}

// Delete marks a key for deletion in the batch
func (b *AuthBatch) Delete(key []byte) error {
	return b.inner.Delete(key)
}

// ValueSize returns the amount of data queued up for writing
func (b *AuthBatch) ValueSize() int {
	return b.inner.ValueSize()
}

// Write commits the batch
func (b *AuthBatch) Write() error {
	return b.inner.Write()
}

// Reset clears the batch for reuse
func (b *AuthBatch) Reset() {
	b.inner.Reset()
}

// Replay replays the batch contents on another batch
func (b *AuthBatch) Replay(w ethdb.KeyValueWriter) error {
	return b.inner.Replay(w)
}

// Get retrieves a value from the batch with authentication
func (b *AuthBatch) Get(key []byte) ([]byte, error) {
	val, err := b.authDB.db.Get(key)
	if err != nil {
		return nil, err
	}
	if b.authDB.mac == nil {
		return val, nil
	}

	expectedTag, err := b.authDB.db.Get(genericAuthTagKey(key))
	if err != nil {
		log.Error("Failed to get auth tag", "dbkey", key, "err", err)
		return nil, err
	}

	tag := b.authDB.computeMac(key, val)
	if !hmac.Equal(tag, expectedTag) {
		log.Error("failed to authenticate", "key", key, "val", val)
		return nil, fmt.Errorf("%w: key=%v val=%d", ErrAuthTagMismatch, key, val)
	}
	return val, nil
}
