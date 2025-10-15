package authdb

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"hash"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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

func (d *AuthDB) AuthWriteBlock(batch ethdb.Batch, block *types.Block) error {
	num := block.NumberU64()
	hash := block.Hash()
	blockBytes, err := rlp.EncodeToBytes(block)
	if err != nil {
		return fmt.Errorf("failed to encode block: %w", err)
	}
	// `rawdb.WriteBlock()` will store Body and Header separately under different prefixes
	// We store the (encoded) block content in one-piece here under a new db key.
	blockKey := blockKey(num, hash)
	if err := batch.Put(blockKey, blockBytes); err != nil {
		return fmt.Errorf("fail to put block with number=%d, hash=%s: %w", num, hash, err)
	}
	if d.mac == nil {
		return nil
	}

	d.mac.Write(blockKey)
	d.mac.Write(blockBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if err := batch.Put(blockAuthTagKey(num, hash), tag); err != nil {
		return fmt.Errorf("fail to put block auth tag with number=%d, hash=%s: %w", num, hash, err)
	}

	return nil
}

func (d *AuthDB) AuthWriteNextHotshotBlockNum(batch ethdb.Batch, num uint64) error {
	// Add the next hotshot block number to the auth db
	if err := batch.Put(nextHotshotBlockNumKey, EncodeUint64(num)); err != nil {
		return fmt.Errorf("failed to put nextHotshotBlockNum: %w", err)
	}
	if d.mac == nil {
		return nil
	}

	// Only if tee is enalbed, add the auth tag to the auth db
	d.mac.Write(nextHotshotBlockNumKey)
	d.mac.Write(EncodeUint64(num))
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if err := batch.Put(nextHotshotBlockNumAuthTagKey, tag); err != nil {
		return fmt.Errorf("failed to put nextHotshotBlockNumAuthTag: %w", err)
	}

	return nil
}

func (d *AuthDB) AuthReadNextHotshotBlockNum() (uint64, error) {
	numBytes, err := d.db.Get(nextHotshotBlockNumKey)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get nextHotshotBlockNum: %w", err)
	}

	num, err := DecodeUint64(numBytes)
	if err != nil {
		return 0, fmt.Errorf("failed to decode nextHotshotBlockNum: %w", err)
	}

	if d.mac == nil {
		return num, nil
	}

	expectedTag, err := d.db.Get(nextHotshotBlockNumAuthTagKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get nextHotshotBlockNumAuthTag: %w", err)
	}

	// verify the auth tag
	d.mac.Write(nextHotshotBlockNumKey)
	d.mac.Write(numBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if !hmac.Equal(tag, expectedTag) {
		return 0, fmt.Errorf("failed to verify nextHotshotBlockNumAuthTag for blockNum: %d", num)
	}

	return num, nil
}

// Delayed message fetcher's FromBlock info
func (d *AuthDB) AuthWriteFromBlock(batch ethdb.Batch, fromBlk uint64) error {
	if err := batch.Put(fromBlockKey, EncodeUint64(fromBlk)); err != nil {
		return fmt.Errorf("failed to put delayedMessageFetcherFromBlock: %w", err)
	}
	if d.mac == nil {
		return nil
	}
	d.mac.Write(fromBlockKey)
	d.mac.Write(EncodeUint64(fromBlk))
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if err := batch.Put(fromBlockAuthTagKey, tag); err != nil {
		return fmt.Errorf("failed to put delayedMessageFetcherFromBlockAuthTag for fromBlock: %d", fromBlk)
	}

	return nil
}

// Delayed message fetcher's FromBlock info
func (d *AuthDB) AuthReadFromBlock() (uint64, error) {
	numBytes, err := d.db.Get(fromBlockKey)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get fromBlock: %w", err)
	}
	fromBlk, err := DecodeUint64(numBytes)
	if err != nil {
		return 0, fmt.Errorf("failed to decode delayedMessageFetcherFromBlock: %w", err)
	}

	if d.mac == nil {
		return fromBlk, nil
	}

	expectedTag, err := d.db.Get(fromBlockAuthTagKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get delayedMessageFetcherFromBlockAuthTag: %w", err)
	}

	// verify the auth tag
	d.mac.Write(fromBlockKey)
	d.mac.Write(numBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if !hmac.Equal(tag, expectedTag) {
		return 0, fmt.Errorf("failed to verify delayedMessageFetcherFromBlockAuthTag for fromBlock: %d", fromBlk)
	}

	return fromBlk, nil
}

// Batcher address montior's InitAddresses info
func (d *AuthDB) AuthWriteInitAddresses(batch ethdb.Batch, addrs []common.Address) error {
	if len(addrs) == 0 {
		return nil
	}
	addrsBytes, err := rlp.EncodeToBytes(addrs)
	if err != nil {
		return fmt.Errorf("failed to encode addrs: %w", err)
	}
	if err := batch.Put(initAddressesKey, addrsBytes); err != nil {
		return fmt.Errorf("failed to put addrs: %w", err)
	}

	if d.mac == nil {
		return nil
	}

	d.mac.Write(initAddressesKey)
	d.mac.Write(addrsBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if err := batch.Put(initAddressesAuthTagKey, tag); err != nil {
		return fmt.Errorf("failed to put addrs: %w", err)
	}
	return nil
}

// Batcher address montior's InitAddresses info
func (d *AuthDB) AuthReadInitAddresses() ([]common.Address, error) {
	addrsBytes, err := d.db.Get(initAddressesKey)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			// nolint:nilerr
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get init addrs: %w", err)
	}

	addrs := []common.Address{}
	err = rlp.DecodeBytes(addrsBytes, &addrs)
	if err != nil {
		return nil, fmt.Errorf("failed to decode addrs: %w", err)
	}

	if d.mac == nil {
		return addrs, nil
	}

	expectedTag, err := d.db.Get(initAddressesAuthTagKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get addrs: %w", err)
	}

	// verify the auth tag
	d.mac.Write(initAddressesKey)
	d.mac.Write(addrsBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if !hmac.Equal(tag, expectedTag) {
		return nil, fmt.Errorf("failed to verify addrsAuthTag for addrs: %d", addrs)
	}

	return addrs, nil
}

// Batcher address monitor's Events info
// We accept RLP-encoded events to avoid cyclic dependency since `BatcherAddrUpdate struct`
// is defined in `arbnode` which will depend on this function
func (d *AuthDB) AuthWriteEvents(batch ethdb.Batch, eventsBytes []byte) error {
	if err := batch.Put(eventsKey, eventsBytes); err != nil {
		return fmt.Errorf("failed to put events: %w", err)
	}

	if d.mac == nil {
		return nil
	}

	d.mac.Write(eventsKey)
	d.mac.Write(eventsBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if err := batch.Put(eventsAuthTagKey, tag); err != nil {
		return fmt.Errorf("failed to put events: %w", err)
	}
	return nil
}

// Batcher address monitor's Events info
func (d *AuthDB) AuthReadEvents() ([]byte, error) {
	eventsBytes, err := d.db.Get(eventsKey)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			// nolint:nilerr
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	if d.mac == nil {
		return eventsBytes, nil
	}

	expectedTag, err := d.db.Get(eventsAuthTagKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	// verify the auth tag
	d.mac.Write(eventsKey)
	d.mac.Write(eventsBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if !hmac.Equal(tag, expectedTag) {
		return nil, fmt.Errorf("failed to verify events auth tag for events bytes: %d", eventsBytes)
	}

	return eventsBytes, nil
}

// Batcher address monitor's LastProcessedHeight info
func (d *AuthDB) AuthWriteLastProcessedHeight(batch ethdb.Batch, height uint64) error {
	if err := batch.Put(lastProcessedHeightKey, EncodeUint64(height)); err != nil {
		return fmt.Errorf("failed to put last processed height: %w", err)
	}

	if d.mac == nil {
		return nil
	}

	d.mac.Write(lastProcessedHeightKey)
	d.mac.Write(EncodeUint64(height))
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if err := batch.Put(lastProcessedHeightAuthTagKey, tag); err != nil {
		return fmt.Errorf("failed to put last processed height: %w", err)
	}
	return nil
}

// Batcher address monitor's LastProcessedHeight info
func (d *AuthDB) AuthReadLastProcessedHeight() (uint64, error) {
	heightBytes, err := d.db.Get(lastProcessedHeightKey)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get last processed height: %w", err)
	}
	height, err := DecodeUint64(heightBytes)
	if err != nil {
		return 0, fmt.Errorf("failed to decode last processed height: %w", err)
	}

	if d.mac == nil {
		return height, nil
	}

	expectedTag, err := d.db.Get(lastProcessedHeightAuthTagKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get last processed height: %w", err)
	}

	// verify the auth tag
	d.mac.Write(lastProcessedHeightKey)
	d.mac.Write(heightBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if !hmac.Equal(tag, expectedTag) {
		return 0, fmt.Errorf("failed to verify last processed height auth tag for height: %d", height)
	}

	return height, nil
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
	return d.db.NewBatch()
}

func (d *AuthDB) NewBatchWithSize(size int) ethdb.Batch {
	return d.db.NewBatchWithSize(size)
}

func (d *AuthDB) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	return d.db.NewIterator(prefix, start)
}

func (d *AuthDB) WasmDataBase() (ethdb.KeyValueStore, uint32) {
	return d.db.WasmDataBase()
}

func (d *AuthDB) WasmTargets() []ethdb.WasmTarget {
	return d.db.WasmTargets()
}

func (d *AuthDB) Put(key []byte, value []byte) error {
	return d.db.Put(key, value)
}

func (d *AuthDB) Has(key []byte) (bool, error) {
	// TODO: Intercepts the calls you care about
	return d.db.Has(key)
}

func (d *AuthDB) Get(key []byte) ([]byte, error) {
	// TODO: Intercepts the calls you care about
	return d.db.Get(key)
}
