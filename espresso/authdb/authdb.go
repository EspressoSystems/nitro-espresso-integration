package authdb

import (
	"bytes"
	"crypto/hmac"
	"errors"
	"fmt"
	"hash"
	"time"

	"github.com/ethereum/go-ethereum/common"
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
	// ErrNoAncients is returned when no ancients data is stored in the database
	ErrNoAncients = errors.New("no ancients data is stored in the database")
)

type AuthDB struct {
	ethdb.Database           // Embedded - inherits methods not needing authentication
	mac            hash.Hash // HMAC func or nil to disable authentication

	// Tag freezer stores authentication tags in separate ancient store
	tagFreezer *rawdb.Freezer

	// isAuthReadsDisabled is a flag to disable auth reads
	// Its used during initial bootstrapping to avoid reading auth tags so that we can
	// add new tags using the new tmac key from a different enclave code
	isAuthReadsDisabled bool
}

func NewAuthDB(db ethdb.Database, mac hash.Hash, isAuthReadsDisabled bool) (AuthDB, error) {
	return newAuthDBWithFreezerTables(db, mac, authTagTableNoSnappy, isAuthReadsDisabled)
}

func newAuthDBWithFreezerTables(db ethdb.Database, mac hash.Hash, tagTables map[string]bool, isAuthReadsDisabled bool) (AuthDB, error) {
	if db == nil {
		return AuthDB{}, errors.New("db is nil during authdb creation")
	}

	authDB := AuthDB{
		Database:            db,
		mac:                 mac,
		isAuthReadsDisabled: isAuthReadsDisabled,
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

func (d *AuthDB) Ancient(kind string, number uint64) ([]byte, error) {
	data, err := d.Database.Ancient(kind, number)
	if err != nil {
		return nil, err
	}

	if d.mac == nil {
		return data, nil
	}

	if err = d.verifyAncientTag(kind, number, data); err != nil {
		return nil, err
	}
	return data, nil
}

// computeMacForAncient computes HMAC for kind, number, and data
func (d *AuthDB) computeMacForAncient(kind string, number uint64, data []byte) []byte {
	d.mac.Reset()
	d.mac.Write([]byte(kind))
	d.mac.Write(EncodeUint64(number))
	d.mac.Write(data)
	return d.mac.Sum(nil)
}

func (d *AuthDB) verify(key []byte, val []byte) error {
	if d.isAuthReadsDisabled {
		// We return true because we don't want to fail the read operation
		return nil
	}

	expectedTag, err := d.Database.Get(genericAuthTagKey(key))
	if err != nil {
		return fmt.Errorf("failed to get auth tag key:%b, err:%w", key, err)
	}

	tag := d.computeMac(key, val)

	if !hmac.Equal(expectedTag, tag) {
		return fmt.Errorf("%w: key=%v val=%d", ErrAuthTagMismatch, key, val)
	}
	return nil
}

// verifyAncientTag reads the tag from the tag store and verifies it matches the data
func (d *AuthDB) verifyAncientTag(kind string, number uint64, data []byte) error {
	if d.isAuthReadsDisabled {
		// We return true because we don't want to fail the read operation
		return nil
	}
	// Get the corresponding tag table name
	tagTable, ok := getTagTable(kind)
	if !ok {
		return fmt.Errorf("%w: unknown table kind: %s", errors.ErrUnsupported, kind)
	}

	// Retrieve stored tag from tag freezer
	if d.tagFreezer == nil {
		return errors.New("tag freezer not initialized")
	}

	storedTag, err := d.tagFreezer.Ancient(tagTable, number)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAuthTagMissing, err)
	}

	// Compute expected tag
	expectedTag := d.computeMacForAncient(kind, number, data)

	// Verify tag
	if !hmac.Equal(expectedTag, storedTag) {
		return fmt.Errorf("%w: kind=%s number=%d", ErrAuthTagMismatch, kind, number)
	}

	return nil
}

func (d *AuthDB) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	items, err := d.Database.AncientRange(kind, start, count, maxBytes)
	if err != nil {
		return nil, err
	}

	if d.mac == nil {
		return items, nil
	}

	for i, data := range items {
		// #nosec G115 -- i is guaranteed non-negative
		err := d.verifyAncientTag(kind, start+uint64(i), data)
		if err != nil {
			return nil, err
		}
	}

	return items, nil
}

// Ancients, Tail, AncientSize inherited from embedded Database - unauthenticated but not security-sensitive

func (d *AuthDB) ReadAncients(fn func(ethdb.AncientReaderOp) error) error {
	return d.Database.ReadAncients(func(op ethdb.AncientReaderOp) error {
		return fn(&AuthAncientReaderOp{AuthDB: d})
	})
}

// AuthAncientReaderOp wraps AncientReaderOp to provide authenticated reads
// Embedding authDB provides all methods automatically
type AuthAncientReaderOp struct {
	*AuthDB // Embedded - all ancient read methods delegated
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
		return d.Database.ModifyAncients(fn)
	}

	// Create our wrapper that collects tag writes
	var authOp *AuthAncientWriteOp

	// Execute main writes
	writeSize, err := d.Database.ModifyAncients(func(op ethdb.AncientWriteOp) error {
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
	// Truncate main database first
	old, err := d.Database.TruncateHead(n)
	if err != nil {
		return old, fmt.Errorf("failed to truncate main database head: %w", err)
	}

	// No tag freezer or auth disabled - nothing to sync
	if d.mac == nil || d.tagFreezer == nil {
		return old, nil
	}

	// Get current tag count
	items, err := d.tagFreezer.Ancients()
	if err != nil {
		log.Crit("Failed to get tag freezer ancients count after main truncation",
			"n", n, "error", err)
		return old, fmt.Errorf("failed to get tag freezer ancients count: %w", err)
	}

	// Check for missing tags - database already corrupt
	if items < n {
		log.Crit("Tag freezer has fewer items than main database after truncation",
			"tag_items", items, "data_items", n, "old", old)
		return old, fmt.Errorf("tag count mismatch: have %d tags but %d data items", items, n)
	}

	// Already in sync
	if items == n {
		return old, nil
	}

	// Truncate excess tags
	_, err = d.tagFreezer.TruncateHead(n)
	if err != nil {
		log.Crit("Failed to truncate tag freezer head after main truncation succeeded",
			"n", n, "old", old, "error", err)
		return old, fmt.Errorf("tag freezer truncate failed after main truncate: %w", err)
	}
	return old, nil
}

func (d *AuthDB) TruncateTail(n uint64) (uint64, error) {
	// Truncate main database first
	old, err := d.Database.TruncateTail(n)
	if err != nil {
		return old, fmt.Errorf("failed to truncate main database tail: %w", err)
	}

	// No tag freezer or auth disabled - nothing to sync
	if d.mac == nil || d.tagFreezer == nil {
		return old, nil
	}

	// Get current tag state
	items, err := d.tagFreezer.Ancients()
	if err != nil {
		log.Crit("Failed to get tag freezer ancients count after main truncation",
			"n", n, "error", err)
		return old, fmt.Errorf("failed to get tag freezer ancients count: %w", err)
	}

	tail, err := d.tagFreezer.Tail()
	if err != nil {
		log.Crit("Failed to get tag freezer tail after main truncation",
			"n", n, "error", err)
		return old, fmt.Errorf("failed to get tag freezer tail: %w", err)
	}

	// Tag tail ahead of requested position - database corrupt
	if tail > n {
		log.Crit("Tag freezer tail is ahead of main database after TruncateTail",
			"tag_tail", tail, "requested_n", n, "old", old)
		return old, fmt.Errorf("tag tail mismatch: tag_tail=%d > n=%d", tail, n)
	}

	// Already in sync
	if tail == n {
		return old, nil
	}

	// Truncate tail to match main database
	if items > 0 {
		_, err = d.tagFreezer.TruncateTail(n)
		if err != nil {
			log.Crit("Failed to truncate tag freezer tail after main truncation succeeded",
				"n", n, "old", old, "error", err)
			return old, fmt.Errorf("tag freezer truncate failed after main truncate: %w", err)
		}
	}
	return old, nil
}

func (d *AuthDB) Sync() error {
	err := d.Database.Sync()
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

// AncientDatadir, Stat inherited from embedded Database

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
	if err := d.Database.Close(); err != nil {
		errs = append(errs, fmt.Errorf("main db close: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

// Compact, Delete, DeleteRange inherited from embedded ethdb.Database

func (d *AuthDB) NewBatch() ethdb.Batch {
	inner := d.Database.NewBatch()
	return &AuthBatch{Batch: inner, authDB: d}
}

func (d *AuthDB) NewBatchWithSize(size int) ethdb.Batch {
	inner := d.Database.NewBatchWithSize(size)
	return &AuthBatch{Batch: inner, authDB: d}
}

// WasmDataBase, WasmTargets inherited from embedded Database - pass through for in-memory fraud game simulation

func (d *AuthDB) Put(key []byte, value []byte) error {
	// Always store the actual data first
	err := d.Database.Put(key, value)
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

	err = d.Database.Put(genericAuthTagKey(key), tag)
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
	val, err := d.Database.Get(key)
	if err != nil {
		return nil, err
	}

	if d.mac == nil {
		return val, nil
	}

	if err := d.verify(key, val); err != nil {
		return nil, fmt.Errorf("failed to authenticate: key=%v val=%d err=%w", key, val, err)
	}

	return val, nil
}

func (d *AuthDB) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	inner := d.Database.NewIterator(prefix, start)
	it := NewAuthIterator(inner, d)
	return &it
}

type AuthIterator struct {
	ethdb.Iterator // Embedded - inherits Error, Key, Release
	db             *AuthDB
}

func NewAuthIterator(inner ethdb.Iterator, db *AuthDB) AuthIterator {
	return AuthIterator{Iterator: inner, db: db}
}

func (it *AuthIterator) Next() bool {
	for it.Iterator.Next() {
		// Skip auth tag keys that end with "-tag"
		key := it.Iterator.Key()
		if !bytes.HasSuffix(key, genericAuthTagSuffix) {
			return true
		}
	}
	return false
}

func (it *AuthIterator) Value() []byte {
	key := it.Key()
	val := it.Iterator.Value()
	if it.db.mac == nil {
		return val
	}

	if err := it.db.verify(key, val); err != nil {
		log.Error("failed to authenticate", "key", key, "val", val, "err", err)
		return nil
	}
	return val
}

// AuthBatch wraps ethdb.Batch to provide authenticated batch operations
type AuthBatch struct {
	ethdb.Batch // Embedded - inherits Delete, ValueSize, Write, Reset, Replay
	authDB      *AuthDB
}

// Put adds a key-value pair to the batch
func (b *AuthBatch) Put(key []byte, value []byte) error {
	if err := b.Batch.Put(key, value); err != nil {
		return err
	}

	if b.authDB.mac != nil {
		tag := b.authDB.computeMac(key, value)

		if err := b.Batch.Put(genericAuthTagKey(key), tag); err != nil {
			return fmt.Errorf("failed to put auth tag for key %s: %w", key, err)
		}

	}
	return nil
}

// Get retrieves a value from the batch with authentication
func (b *AuthBatch) Get(key []byte) ([]byte, error) {
	val, err := b.authDB.Database.Get(key)
	if err != nil {
		return nil, err
	}
	if b.authDB.mac == nil {
		return val, nil
	}

	if err := b.authDB.verify(key, val); err != nil {
		return nil, fmt.Errorf("failed to authenticate: key=%v val=%d err=%w", key, val, err)
	}
	return val, nil
}

// InitAuthTags initializes auth tags for all keys in the database
// when the node is started in the snapshot mode (which means when its initially provided a snapshot from another TEE code hash/non-tee node)
func (d *AuthDB) InitAuthTagsDatabase(batchSize int) error {
	var (
		prefix     []byte
		start      []byte
		startTime  = time.Now()
		loggedTime = time.Now()
		count      = 0
	)
	log.Info("Starting adding auth tags to the database")

	// Use the raw database iterator to avoid per-key auth verification during initialization
	it := d.Database.NewIterator(prefix, start)
	defer it.Release()

	// Buffer writes in a raw batch to reduce I/O and avoid writing tags-for-tags
	batch := d.Database.NewBatch()

	// For each key value pair in the database add an auth tag
	for it.Next() {
		key := it.Key()
		// Skip keys that are themselves tag entries
		if bytes.HasSuffix(key, genericAuthTagSuffix) {
			continue
		}
		value := it.Value()

		// Compute and write the tag in the raw batch
		tag := d.computeMac(key, value)
		if err := batch.Put(genericAuthTagKey(key), tag); err != nil {
			batch.Reset()
			return fmt.Errorf("failed to put auth tag for key %v: %w", key, err)
		}
		count++

		// Periodically flush to avoid huge batches and reduce fsync overhead
		if count%batchSize == 0 {
			if err := batch.Write(); err != nil {
				batch.Reset()
				return fmt.Errorf("failed to write auth tag batch: %w", err)
			}
			batch.Reset()
		}
		// if 5 minuetes have passed still log
		if time.Since(loggedTime) > 5*time.Minute {
			log.Info("Progress adding auth tags", "count", count, "elapsed", common.PrettyDuration(time.Since(startTime)))
			loggedTime = time.Now()
		}
	}

	// Flush any remaining buffered tags
	if err := batch.Write(); err != nil {
		batch.Reset()
		return fmt.Errorf("failed to write final auth tag batch: %w", err)
	}
	log.Info("Successfully added auth tags to the database")
	return nil
}

// InitAncientAuthTags initializes auth tags for all ancients in the database
// when the node is started in the snapshot mode (which means when its initially provided a snapshot from another TEE code hash/non-tee node)
func (d *AuthDB) InitAncientAuthTags(batchSize uint64) error {
	if batchSize == 0 {
		return fmt.Errorf("batchSize must be greater than 0")
	}
	firstBlock, err := d.Database.Tail()
	if err != nil {
		return err
	}
	numAncients, err := d.Database.Ancients()
	if err != nil {
		return err
	}

	if numAncients == 0 {
		return nil
	}

	log.Info("Starting adding ancient auth tags")

	for offset := uint64(0); offset < numAncients; offset += batchSize {
		count := batchSize

		if offset+batchSize > numAncients {
			count = numAncients - offset
		}

		currentBlock := firstBlock + offset

		hashData, blockBodyData, receiptData, headerData, err := d.readChainAncients(currentBlock, count)
		if err != nil {
			return err
		}

		_, err = d.tagFreezer.ModifyAncients(func(tagOp ethdb.AncientWriteOp) error {
			for i := uint64(0); i < count; i++ {
				num := currentBlock + uint64(i)

				if err := tagOp.AppendRaw(AuthTagHashTable, num, hashData[i]); err != nil {
					return err
				}
				if err := tagOp.AppendRaw(AuthTagHeaderTable, num, headerData[i]); err != nil {
					return err
				}
				if err := tagOp.AppendRaw(AuthTagBodiesTable, num, blockBodyData[i]); err != nil {
					return err
				}
				if err := tagOp.AppendRaw(AuthTagReceiptTable, num, receiptData[i]); err != nil {
					return err
				}
			}
			return nil
		})

		if err != nil {
			return err
		}

		log.Info("Processed batch", "from", currentBlock, "count", count, "num_ancients", numAncients)
	}

	log.Info("Successfully added auth tags to ancients",
		"from", firstBlock, "count", numAncients)
	return nil
}

func (d *AuthDB) readChainAncients(firstBlock, numAncients uint64) (hashData, bodyData, receiptData, headerData [][]byte, err error) {

	hashData, err = d.Database.AncientRange(rawdb.ChainFreezerHashTable, firstBlock, numAncients, 0)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	bodyData, err = d.Database.AncientRange(rawdb.ChainFreezerBodiesTable, firstBlock, numAncients, 0)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	receiptData, err = d.Database.AncientRange(rawdb.ChainFreezerReceiptTable, firstBlock, numAncients, 0)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	headerData, err = d.Database.AncientRange(rawdb.ChainFreezerHeaderTable, firstBlock, numAncients, 0)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	return hashData, bodyData, receiptData, headerData, nil
}
