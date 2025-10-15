package authdb

import (
	"bytes"
	"crypto/hmac"
	"errors"
	"fmt"
	"hash"

	"github.com/ethereum/go-ethereum/common"
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

// // Different from rawdb.WriteBlock which write header and body separately,
// // we write the full RLP-encoded block under a different key
// func (d *AuthDB) AuthWriteBlock(batch ethdb.Batch, block *types.Block) error {
// 	num := block.NumberU64()
// 	hash := block.Hash()
// 	blockBytes, err := rlp.EncodeToBytes(block)
// 	if err != nil {
// 		return fmt.Errorf("failed to encode block: %w", err)
// 	}
// 	// `rawdb.WriteBlock()` will store Body and Header separately under different prefixes
// 	// We store the (encoded) block content in one-piece here under a new db key.
// 	blockKey := blockKey(num, hash)
// 	if err := batch.Put(blockKey, blockBytes); err != nil {
// 		return fmt.Errorf("fail to put block with number=%d, hash=%s: %w", num, hash, err)
// 	}
// 	if d.mac == nil {
// 		return nil
// 	}

// 	d.mac.Write(blockKey)
// 	d.mac.Write(blockBytes)
// 	tag := d.mac.Sum(nil)
// 	d.mac.Reset()
// 	if err := batch.Put(blockAuthTagKey(num, hash), tag); err != nil {
// 		return fmt.Errorf("fail to put block auth tag with number=%d, hash=%s: %w", num, hash, err)
// 	}

// 	return nil
// }

// // Same as rawdb.WriteHeader except with additional auth tag
// func (d *AuthDB) AuthWriteHeader(batch ethdb.Batch, header *types.Header) error {
// 	// header writing logic is almost identical to rawdb.WriterHeader except using batch
// 	var (
// 		hash   = header.Hash()
// 		number = header.Number.Uint64()
// 	)
// 	// Write the hash -> number mapping
// 	rawdb.WriteHeaderNumber(batch, hash, number)
// 	// Write the encoded header
// 	data, err := rlp.EncodeToBytes(header)
// 	if err != nil {
// 		log.Crit("Failed to RLP encode header", "err", err)
// 	}
// 	key := headerKey(number, hash)
// 	if err := d.db.Put(key, data); err != nil {
// 		log.Crit("Failed to store header", "err", err)
// 	}

// 	if d.mac == nil {
// 		return nil
// 	}

// 	d.mac.Write(key)
// 	d.mac.Write(data)
// 	tag := d.mac.Sum(nil)
// 	d.mac.Reset()
// 	if err := batch.Put(headerAuthTagKey(number, hash), tag); err != nil {
// 		return fmt.Errorf("fail to put header auth tag with number=%d, hash=%s: %w", number, hash, err)
// 	}
// 	return nil
// }

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

func (d *AuthDB) WasmDataBase() (ethdb.KeyValueStore, uint32) {
	return d.db.WasmDataBase()
}

func (d *AuthDB) WasmTargets() []ethdb.WasmTarget {
	return d.db.WasmTargets()
}

func (d *AuthDB) Put(key []byte, value []byte) error {
	if d.mac == nil {
		return d.db.Put(key, value)
	}

	// We add auth tags to header, body and tx receipts
	switch {
	case bytes.HasPrefix(key, headerPrefix) && len(key) == (len(headerPrefix)+8+common.HashLength):
		number, hash, err := parseUint64AndHash(key, len(headerPrefix))
		if err != nil {
			return err
		}

		d.mac.Write(key)
		d.mac.Write(value)
		tag := d.mac.Sum(nil)
		d.mac.Reset()
		err = d.db.Put(headerAuthTagKey(number, hash), tag)
		if err != nil {
			log.Crit("failed to write header auth tag", "err", err)
			return nil
		}

	case bytes.HasPrefix(key, blockBodyPrefix) && len(key) == (len(blockBodyPrefix)+8+common.HashLength):
		number, hash, err := parseUint64AndHash(key, len(blockBodyPrefix))
		if err != nil {
			return err
		}

		d.mac.Write(key)
		d.mac.Write(value)
		tag := d.mac.Sum(nil)
		d.mac.Reset()
		err = d.db.Put(bodyAuthTagKey(number, hash), tag)
		if err != nil {
			log.Crit("failed to write body auth tag", "err", err)
			return nil
		}

	case bytes.HasPrefix(key, blockReceiptsPrefix) && len(key) == (len(blockReceiptsPrefix)+8+common.HashLength):
		number, hash, err := parseUint64AndHash(key, len(blockReceiptsPrefix))
		if err != nil {
			return err
		}

		d.mac.Write(key)
		d.mac.Write(value)
		tag := d.mac.Sum(nil)
		d.mac.Reset()
		err = d.db.Put(receiptsAuthTagKey(number, hash), tag)
		if err != nil {
			log.Crit("failed to write receipts auth tag", "err", err)
			return nil
		}
	default:
		// pass through the rest
	}
	return d.db.Put(key, value)
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
	// switch-case copied over from rawdb.database.go::InspectDatabase()
	switch {
	case bytes.HasPrefix(key, headerPrefix) && len(key) == (len(headerPrefix)+8+common.HashLength):
		number, hash, err := parseUint64AndHash(key, len(headerPrefix))
		if err != nil {
			return nil, err
		}
		return d.authReadHeader(hash, number)
	case bytes.HasPrefix(key, blockBodyPrefix) && len(key) == (len(blockBodyPrefix)+8+common.HashLength):
		number, hash, err := parseUint64AndHash(key, len(headerPrefix))
		if err != nil {
			return nil, err
		}
		return d.authReadBody(hash, number)
	case bytes.HasPrefix(key, blockReceiptsPrefix) && len(key) == (len(blockReceiptsPrefix)+8+common.HashLength):
		number, hash, err := parseUint64AndHash(key, len(headerPrefix))
		if err != nil {
			return nil, err
		}
		return d.authReadReceipts(hash, number)
	case bytes.HasPrefix(key, headerPrefix) && bytes.HasSuffix(key, headerTDSuffix):
		log.Error("headerTDSuffix is deprecated")
		return nil, fmt.Errorf("headerTDSuffix is deprecated")
	case bytes.HasPrefix(key, headerPrefix) && bytes.HasSuffix(key, headerHashSuffix):
		number, err := DecodeUint64(key[len(headerPrefix) : len(headerPrefix)+8])
		if err != nil {
			log.Error("failed to decode header number", "err", err)
			return nil, err
		}
		hashBytes, err := d.db.Get(headerHashKey(number))
		if err != nil {
			log.Error("failed to get header hash", "err", err)
			return nil, err
		}
		hash := common.BytesToHash(hashBytes)
		return d.authReadHeader(hash, number)
	case bytes.HasPrefix(key, headerNumberPrefix) && len(key) == (len(headerNumberPrefix)+common.HashLength):
		hash := common.BytesToHash(key[len(headerNumberPrefix) : len(headerNumberPrefix)+common.HashLength])
		numberBytes, err := d.db.Get(headerNumberKey(hash))
		if err != nil {
			log.Error("failed to get header number", "err", err)
			return nil, err
		}
		number, err := DecodeUint64(numberBytes)
		if err != nil {
			log.Error("failed to decode header number", "err", err)
			return nil, err
		}
		return d.authReadHeader(hash, number)
	// note: IsLegacyTrieNode is not a read op, skipping
	// case IsLegacyTrieNode(key, it.Value()):
	case bytes.HasPrefix(key, stateIDPrefix) && len(key) == len(stateIDPrefix)+common.HashLength:
		// stateLookups.Add(size)
	case IsAccountTrieNode(key):
		// accountTries.Add(size)
	case IsStorageTrieNode(key):
		// storageTries.Add(size)
	case bytes.HasPrefix(key, CodePrefix) && len(key) == len(CodePrefix)+common.HashLength:
		// codes.Add(size)
	case bytes.HasPrefix(key, txLookupPrefix) && len(key) == (len(txLookupPrefix)+common.HashLength):
		// txLookups.Add(size)
	case bytes.HasPrefix(key, SnapshotAccountPrefix) && len(key) == (len(SnapshotAccountPrefix)+common.HashLength):
		// accountSnaps.Add(size)
	case bytes.HasPrefix(key, SnapshotStoragePrefix) && len(key) == (len(SnapshotStoragePrefix)+2*common.HashLength):
		// storageSnaps.Add(size)
	case bytes.HasPrefix(key, PreimagePrefix) && len(key) == (len(PreimagePrefix)+common.HashLength):
		// preimages.Add(size)
	case bytes.HasPrefix(key, configPrefix) && len(key) == (len(configPrefix)+common.HashLength):
		// metadata.Add(size)
	case bytes.HasPrefix(key, genesisPrefix) && len(key) == (len(genesisPrefix)+common.HashLength):
		// metadata.Add(size)
	case bytes.HasPrefix(key, bloomBitsPrefix) && len(key) == (len(bloomBitsPrefix)+10+common.HashLength):
		// bloomBits.Add(size)
	case bytes.HasPrefix(key, BloomBitsIndexPrefix):
		// bloomBits.Add(size)
	case bytes.HasPrefix(key, skeletonHeaderPrefix) && len(key) == (len(skeletonHeaderPrefix)+8):
		// beaconHeaders.Add(size)
	case bytes.HasPrefix(key, CliqueSnapshotPrefix) && len(key) == 7+common.HashLength:
		// cliqueSnaps.Add(size)
	case bytes.HasPrefix(key, ChtTablePrefix) ||
		bytes.HasPrefix(key, ChtIndexTablePrefix) ||
		bytes.HasPrefix(key, ChtPrefix): // Canonical hash trie
		// chtTrieNodes.Add(size)
	case bytes.HasPrefix(key, BloomTrieTablePrefix) ||
		bytes.HasPrefix(key, BloomTrieIndexPrefix) ||
		bytes.HasPrefix(key, BloomTriePrefix): // Bloomtrie sub
		// bloomTrieNodes.Add(size)

	// Verkle trie data is detected, determine the sub-category
	case bytes.HasPrefix(key, VerklePrefix):
		remain := key[len(VerklePrefix):]
		switch {
		case IsAccountTrieNode(remain):
			// verkleTries.Add(size)
		case bytes.HasPrefix(remain, stateIDPrefix) && len(remain) == len(stateIDPrefix)+common.HashLength:
			// verkleStateLookups.Add(size)
		case bytes.Equal(remain, persistentStateIDKey):
			// metadata.Add(size)
		case bytes.Equal(remain, trieJournalKey):
			// metadata.Add(size)
		case bytes.Equal(remain, snapSyncStatusFlagKey):
			// metadata.Add(size)
		default:
			// unaccounted.Add(size)
		}
	default:
		for range [][]byte{
			databaseVersionKey, headHeaderKey, headBlockKey, headFastBlockKey, headFinalizedBlockKey,
			lastPivotKey, fastTrieProgressKey, snapshotDisabledKey, SnapshotRootKey, snapshotJournalKey,
			snapshotGeneratorKey, snapshotRecoveryKey, txIndexTailKey, fastTxLookupLimitKey,
			uncleanShutdownKey, badBlockKey, transitionStatusKey, skeletonSyncStatusKey,
			persistentStateIDKey, trieJournalKey, snapshotSyncStatusKey, snapSyncStatusFlagKey,
		} {
			//
		}
	}

	// TODO: decide how to deal with freezer and Ancient reads
	// var freezers = []string{ChainFreezerName, MerkleStateFreezerName, VerkleStateFreezerName}
	// for _, freezer := range freezers {
	// 	switch freezer {
	// 	case ChainFreezerName:
	// 		info, err := inspect(ChainFreezerName, chainFreezerNoSnappy, db)
	// 		if err != nil {
	// 			return nil, err
	// 		}
	// 		infos = append(infos, info)

	// 	case MerkleStateFreezerName, VerkleStateFreezerName:
	// 		datadir, err := db.AncientDatadir()
	// 		if err != nil {
	// 			return nil, err
	// 		}
	// 		f, err := NewStateFreezer(datadir, freezer == VerkleStateFreezerName, true)
	// 		if err != nil {
	// 			continue // might be possible the state freezer is not existent
	// 		}
	// 		defer f.Close()

	// 		info, err := inspect(freezer, stateFreezerNoSnappy, f)
	// 		if err != nil {
	// 			return nil, err
	// 		}
	// 		infos = append(infos, info)

	// 	default:
	// 		return nil, fmt.Errorf("unknown freezer, supported ones: %v", freezers)
	// 	}
	// }

	// TODO: Intercepts the calls you care about
	return d.db.Get(key)
}

func (d *AuthDB) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	inner := d.db.NewIterator(prefix, start)
	it := NewAuthIterator(inner, d)
	return &it
}

type AuthIterator struct {
	inner ethdb.Iterator
	db    ethdb.Database
}

func NewAuthIterator(inner ethdb.Iterator, db ethdb.Database) AuthIterator {
	return AuthIterator{inner: inner, db: db}
}

func (it *AuthIterator) Next() bool {
	return it.inner.Next()
}

func (it *AuthIterator) Error() error {
	return it.inner.Error()
}

func (it *AuthIterator) Key() []byte {
	return it.inner.Key()
}

func (it *AuthIterator) Value() []byte {
	key := it.Key()

	val, err := it.db.Get(key)
	if err != nil {
		log.Error("AuthRead failed during AuthIterator.Value()", "err", err)
		return nil
	}
	return val
}

func (it *AuthIterator) Release() {
	it.inner.Release()
}
