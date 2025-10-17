package authdb

import (
	"bytes"
	"crypto/hmac"
	"errors"
	"fmt"
	"hash"

	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/util/dbutil"
)

type AuthDB struct {
	db  ethdb.Database
	mac hash.Hash // HMAC func or nil to disable authentication
}

func NewAuthDB(db ethdb.Database, mac hash.Hash) (AuthDB, error) {
	if db == nil {
		return AuthDB{}, errors.New("db is nil")
	}
	if mac == nil {
		log.Warn("new AuthDB with authentication disabled")
	}
	return AuthDB{db: db, mac: mac}, nil
}

func (d *AuthDB) Ancient(kind string, number uint64) ([]byte, error) {
	// TODO: We should intercept the call and return nil?
	return d.db.Ancient(kind, number)
}

func (d *AuthDB) AncientDatadir() (string, error) {
	return d.db.AncientDatadir()
}

func (d *AuthDB) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	// TODO: We should intercept the call and return nil?
	return d.db.AncientRange(kind, start, count, maxBytes)
}

func (d *AuthDB) Ancients() (uint64, error) {
	// TODO: We should intercept the call and return nil?
	return d.db.Ancients()
}

func (d *AuthDB) AncientSize(kind string) (uint64, error) {
	return d.db.AncientSize(kind)
}

func (d *AuthDB) HasAncient(kind string, number uint64) (bool, error) {
	// TODO: We should intercept the call and return nil?
	return d.db.HasAncient(kind, number)
}

func (d *AuthDB) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (int64, error) {
	return d.db.ModifyAncients(fn)
}

func (d *AuthDB) ReadAncients(fn func(ethdb.AncientReaderOp) error) error {
	// TODO: We should intercept the call and return nil?
	return d.db.ReadAncients(fn)
}

func (d *AuthDB) Tail() (uint64, error) {
	return d.db.Tail()
}

func (d *AuthDB) Stat() (string, error) {
	return d.db.Stat()
}

func (d *AuthDB) Sync() error {
	return d.db.Sync()
}

func (d *AuthDB) TruncateHead(n uint64) (uint64, error) {
	return d.db.TruncateHead(n)
}

func (d *AuthDB) TruncateTail(n uint64) (uint64, error) {
	return d.db.TruncateTail(n)
}

func (d *AuthDB) Close() error {
	return d.db.Close()
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
	d.mac.Write(key)
	d.mac.Write(value)
	tag := d.mac.Sum(nil)
	d.mac.Reset()

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

	d.mac.Write(key)
	d.mac.Write(val)
	tag := d.mac.Sum(nil)
	d.mac.Reset()

	if !hmac.Equal(tag, expectedTag) {
		log.Error("failed to authenticate", "key", key, "val", val)
		return nil, fmt.Errorf("failed to authenticate body, key: %v, val: %d", key, val)
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

func NewAuthIterator(inner ethdb.Iterator, db ethdb.Database) AuthIterator {
	authDB, _ := db.(*AuthDB)
	return AuthIterator{inner: inner, db: authDB}
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
	if it.db.mac == nil || !bytes.HasSuffix(key, genericAuthTagSuffix) {
		return val
	}

	expectedTag, err := it.db.Get(genericAuthTagKey(key))
	if err != nil {
		log.Error("Failed to get auth tag", "dbkey", key, "err", err)
		return nil
	}

	it.db.mac.Write(key)
	it.db.mac.Write(val)
	tag := it.db.mac.Sum(nil)
	it.db.mac.Reset()

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
	// Track kv pairs for auth tag generation on write (inner's tracker is private)
	entries map[string][]byte
}

// Put adds a key-value pair to the batch
func (b *AuthBatch) Put(key []byte, value []byte) error {
	if b.entries == nil {
		b.entries = make(map[string][]byte)
	}
	b.entries[string(key)] = value
	return b.inner.Put(key, value)
}

// Delete marks a key for deletion in the batch
func (b *AuthBatch) Delete(key []byte) error {
	if b.entries != nil {
		delete(b.entries, string(key))
	}
	return b.inner.Delete(key)
}

// ValueSize returns the amount of data queued up for writing
func (b *AuthBatch) ValueSize() int {
	return b.inner.ValueSize()
}

// Write commits the batch, adding auth tags for each entry if MAC is enabled
func (b *AuthBatch) Write() error {
	if b.authDB.mac != nil && b.entries != nil {
		// Add auth tags for each entry
		for keyStr, value := range b.entries {
			key := []byte(keyStr)
			b.authDB.mac.Write(key)
			b.authDB.mac.Write(value)
			tag := b.authDB.mac.Sum(nil)
			b.authDB.mac.Reset()

			if err := b.inner.Put(genericAuthTagKey(key), tag); err != nil {
				return fmt.Errorf("failed to put auth tag for key %s: %w", keyStr, err)
			}
		}
	}
	return b.inner.Write()
}

// Reset clears the batch for reuse
func (b *AuthBatch) Reset() {
	b.inner.Reset()
	b.entries = nil
}

// Replay replays the batch contents on another batch
func (b *AuthBatch) Replay(w ethdb.KeyValueWriter) error {
	return b.inner.Replay(w)
}

// Get retrieves a value from the batch or underlying database
func (b *AuthBatch) Get(key []byte) ([]byte, error) {
	// Check if this key was recently put in the batch
	if b.entries != nil {
		if value, exists := b.entries[string(key)]; exists {
			return value, nil
		}
	}
	// Fall back to the underlying AuthDB
	return b.authDB.Get(key)
}

// Has checks if a key exists in the batch or underlying database
func (b *AuthBatch) Has(key []byte) (bool, error) {
	// Check if this key was recently put in the batch
	if b.entries != nil {
		if _, exists := b.entries[string(key)]; exists {
			return true, nil
		}
	}
	// Fall back to the underlying AuthDB
	return b.authDB.Has(key)
}
