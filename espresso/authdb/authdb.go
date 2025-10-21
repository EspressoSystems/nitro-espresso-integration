package authdb

import (
	"bytes"
	"crypto/hmac"
	"errors"
	"fmt"
	"hash"
	"math"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

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

	return d.authenticateAncientData(kind, number, data)
}

// computeMac computes HMAC for kind, number, and data
func (d *AuthDB) computeMacForAncient(kind string, number uint64, data []byte) []byte {
	d.mac.Reset()
	d.mac.Write([]byte(kind))
	d.mac.Write(EncodeUint64(number))
	d.mac.Write(data)
	return d.mac.Sum(nil)
}

// authenticateAncientData verifies authentication for ancient data
func (d *AuthDB) authenticateAncientData(kind string, number uint64, data []byte) ([]byte, error) {
	switch kind {
	case rawdb.ChainFreezerHashTable:
		dataSize := len(data) - d.mac.Size()
		expectedTag := data[dataSize:]
		actualData := data[:dataSize]

		tag := d.computeMacForAncient(kind, number, actualData)
		if !hmac.Equal(tag, expectedTag) {
			return nil, fmt.Errorf("failed to verify auth tag for kind: %v, number: %v", kind, number)
		}
		return actualData, nil

	case rawdb.ChainFreezerBodiesTable, rawdb.ChainFreezerHeaderTable, rawdb.ChainFreezerReceiptTable:
		authItem := new(AncientItemWithTag)
		if err := rlp.DecodeBytes(data, authItem); err != nil {
			log.Error("invalid freezer rlp", "err", err)
			return nil, err
		}

		var buf bytes.Buffer
		if err := rlp.Encode(&buf, authItem.item); err != nil {
			log.Error("failed to RLP encode", "err", err)
			return nil, err
		}

		tag := d.computeMacForAncient(kind, number, buf.Bytes())
		if !hmac.Equal(tag, authItem.tag) {
			log.Error("auth tag mismatch in AncientItemWithTag")
			return nil, fmt.Errorf("auth tag mismatch for AncientItemWithTag, kind: %v, number: %v", kind, number)
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("unsupported chain freezer kind: %s", kind)
	}
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
		verifiedData, err := d.authenticateAncientData(kind, number, data)
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
	return d.db.ReadAncients(fn)
}

// the item to be appended in ancient store, with an auth tag
type AncientItemWithTag struct {
	item interface{}
	tag  []byte
}

func NewAncientItemWithTag(kind string, number uint64, item interface{}, mac hash.Hash) (*AncientItemWithTag, error) {
	var buf bytes.Buffer
	if err := rlp.Encode(&buf, item); err != nil {
		log.Error("failed to RLP encode", "err", err)
		return nil, err
	}

	mac.Reset()
	mac.Write([]byte(kind))
	mac.Write(EncodeUint64(number))
	mac.Write(buf.Bytes())
	tag := mac.Sum(nil)
	return &AncientItemWithTag{item: item, tag: tag}, nil
}

// Wrapping AncientWriteOp with auth tags injection
type AuthAncientWriteOp struct {
	inner ethdb.AncientWriteOp
	mac   hash.Hash
}

// tag = HMAC(kind || number || rlp.Encode(item)),
// actual appended/persisted item is AncientItemWithTag
// we first RLP encode the item when computing the tag because we don't know its exact type
func (op AuthAncientWriteOp) Append(kind string, number uint64, item interface{}) error {
	authItem, err := NewAncientItemWithTag(kind, number, item, op.mac)
	if err != nil {
		return err
	}
	return op.inner.Append(kind, number, authItem)
}

// tag = HMAC(kind || number || item)
// actual appended/persisted item is (item || tag)
func (op AuthAncientWriteOp) AppendRaw(kind string, number uint64, item []byte) error {
	op.mac.Reset()
	op.mac.Write([]byte(kind))
	op.mac.Write(EncodeUint64(number))
	op.mac.Write(item)
	tag := op.mac.Sum(nil)

	return op.inner.AppendRaw(kind, number, append(item, tag...))
}

func (d *AuthDB) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (int64, error) {
	authFn := func(op ethdb.AncientWriteOp) error {
		authOp := AuthAncientWriteOp{
			inner: op,
			mac:   d.mac,
		}
		return fn(authOp)
	}
	return d.db.ModifyAncients(authFn)
}

func (d *AuthDB) TruncateHead(n uint64) (uint64, error) {
	return d.db.TruncateHead(n)
}

func (d *AuthDB) TruncateTail(n uint64) (uint64, error) {
	return d.db.TruncateTail(n)
}

func (d *AuthDB) Sync() error {
	return d.db.Sync()
}

func (d *AuthDB) AncientDatadir() (string, error) {
	return d.db.AncientDatadir()
}

func (d *AuthDB) Stat() (string, error) {
	return d.db.Stat()
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
			tag := b.authDB.computeMac(key, value)

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

// InitAuthTags initializes auth tags for all keys in the database
func (d *AuthDB) InitAuthTags() error {
	var (
		prefix    []byte
		start     []byte
		startTime = time.Now()
		logged    = time.Now()
		count     = 0
	)

	it := d.db.NewIterator(prefix, start)
	defer it.Release()

	// For each key value pair in the database add an auth tag
	for it.Next() {
		key := it.Key()
		value := it.Value()
		tag := d.computeMac(key, value)

		if err := d.db.Put(genericAuthTagKey(key), tag); err != nil {
			return fmt.Errorf("failed to put auth tag for key %v: %w", key, err)
		}

		count++
		if time.Since(logged) > 8*time.Second {
			log.Info("Added auth tags to the database", "count", count, "elapsed", common.PrettyDuration(time.Since(startTime)))
			logged = time.Now()
		}
	}

	return nil
}

// InitAncientAuthTags initializes auth tags for all ancient data in the database
func (d *AuthDB) InitAncientAuthTags() error {
	firstBlockNumInAncients, err := d.Tail()
	if err != nil {
		return fmt.Errorf("failed to read tail of ancients :%w", err)
	}
	lastBlockNumInAncients, err := d.Ancients()
	if err != nil {
		return fmt.Errorf("failed to read last block number in ancients :%w", err)
	}
	logged := time.Now()
	startTime := time.Now()

	blockNum := firstBlockNumInAncients
	for blockNum <= lastBlockNumInAncients {
		err := d.readAndModifyChainAncients(blockNum)
		if err != nil {
			return fmt.Errorf("failed to read and modify chain ancients :%w", err)
		}
		err = d.readAndModifyStateAncients(blockNum)
		if err != nil {
			return fmt.Errorf("failed to read and modify state ancients :%w", err)
		}
		if time.Since(logged) > 8*time.Second {
			log.Info("Added auth tags to the database", "count", blockNum, "elapsed", common.PrettyDuration(time.Since(startTime)))
			logged = time.Now()
		}
		blockNum++
	}

	return nil
}

func (d *AuthDB) readAndModifyChainAncients(blockNum uint64) error {
	var hashData, blockBodyData, headerData, receiptData []byte
	var err error
	err = d.db.ReadAncients(func(reader ethdb.AncientReaderOp) error {
		hashData, err = reader.Ancient(rawdb.ChainFreezerHashTable, blockNum)
		if err != nil {
			return err
		}
		blockBodyData, err = reader.Ancient(rawdb.ChainFreezerBodiesTable, blockNum)
		if err != nil {
			return err
		}
		headerData, err = reader.Ancient(rawdb.ChainFreezerHeaderTable, blockNum)
		if err != nil {
			return err
		}
		receiptData, err = reader.Ancient(rawdb.ChainFreezerReceiptTable, blockNum)
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		log.Error("Failed to read ancient data", "err", err)
		return err
	}

	_, err = d.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		if err := op.AppendRaw(rawdb.ChainFreezerHashTable, blockNum, hashData); err != nil {
			return err
		}
		if err := op.AppendRaw(rawdb.ChainFreezerHeaderTable, blockNum, headerData); err != nil {
			log.Error("Failed to append header data to auth db", "err", err)
			return err
		}
		if err := op.AppendRaw(rawdb.ChainFreezerBodiesTable, blockNum, blockBodyData); err != nil {
			log.Error("Failed to append block body data to auth db", "err", err)
			return err
		}
		if err := op.AppendRaw(rawdb.ChainFreezerReceiptTable, blockNum, receiptData); err != nil {
			log.Error("Failed to append receipt data to auth db", "err", err)
			return err
		}
		return nil
	})
	return err
}

func (d *AuthDB) readAndModifyStateAncients(blockNum uint64) error {
	// TODO: Implement this function, note state id is different from block number so we will have to think about that
	return nil
}
