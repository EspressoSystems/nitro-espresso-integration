package authdb

import (
	"crypto/hmac"
	"errors"
	"hash"

	"github.com/ethereum/go-ethereum/ethdb"
)

type AuthDB struct {
	db  ethdb.Database
	mac hash.Hash
}

func NewAuthDB(db ethdb.Database, mac hash.Hash) (AuthDB, error) {
	if db == nil {
		return AuthDB{}, errors.New("db is nil")
	}
	if mac == nil {
		return AuthDB{}, errors.New("HMAC func is nil")
	}
	return AuthDB{db: db, mac: mac}, nil
}

func (d *AuthDB) AuthWriteNextHotshotBlockNum(batch ethdb.Batch, num uint64) error {
	d.mac.Write(NextHotshotBlockNumKey)
	d.mac.Write(EncodeUint64(num))
	tag := d.mac.Sum(nil)
	d.mac.Reset()

	if err := batch.Put(NextHotshotBlockNumKey, num); err != nil {
		return fmt.Errorf("failed to put nextHotshotBlockNum: %w", err)
	}
	if err := batch.Put(NextHotshotBlockNumAuthTagKey(num), tag); err != nil {
		return fmt.Errorf("failed to put nextHotshotBlockNumAuthTag: %w", err)
	}

	return nil
}

func (d *AuthDB) AuthReadNextHotshotBlockNum() (uint64, error) {
	numBytes, err := d.db.Get(NextHotshotBlockNumKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get nextHotshotBlockNum: %w", err)
	}
	num := DecodeUnit64(numBytes)

	expectedTag, err := d.db.Get(NextHotshotBlockNumAuthTagKey(num))
	if err != nil {
		return 0, fmt.Errorf("failed to get nextHotshotBlockNumAuthTag: %w", err)
	}

	// verify the auth tag
	d.mac.Write(NextHotshotBlockNumKey)
	d.mac.Write(numBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if !hmac.Equal(tag, expectedTag) {
		return 0, fmt.Errorf("failed to verify nextHotshotBlockNumAuthTag for blockNum: %d", num)
	}

	return num, nil
}

func (d *AuthDB) AuthWriteDelayedMessageFetchFromBlock(batch ethdb.Batch, fromBlk uint64) error {
	d.mac.Write(DelayedMessageFetcherFromBlockKey)
	d.mac.Write(EncodeUint64(fromBlk))
	tag := d.mac.Sum(nil)
	d.mac.Reset()

	if err := batch.Put(DelayedMessageFetcherFromBlockKey, num); err != nil {
		return fmt.Errorf("failed to put delayedMessageFetcherFromBlock: %w", err)
	}
	if err := batch.Put(DelayedMessageFetcherFromBlockAuthTagKey(fromBlk), tag); err != nil {
		return fmt.Errorf("failed to put delayedMessageFetcherFromBlockAuthTag for fromBlock: %d", fromBlk)
	}

	return nil
}

func (d *AuthDB) AuthReadDelayedMessageFetchFromBlock() (uint64, error) {
	numBytes, err := d.db.Get(DelayedMessageFetcherFromBlockKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get delayedMessageFetcherFromBlock: %w", err)
	}
	fromBlk := DecodeUnit64(numBytes)

	expectedTag, err := d.db.Get(DelayedMessageFetcherFromBlockAuthTagKey(fromBlk))
	if err != nil {
		return 0, fmt.Errorf("failed to get delayedMessageFetcherFromBlockAuthTag: %w", err)
	}

	// verify the auth tag
	d.mac.Write(DelayedMessageFetcherFromBlockKey)
	d.mac.Write(numBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()
	if !hmac.Equal(tag, expectedTag) {
		return 0, fmt.Errorf("failed to verify delayedMessageFetcherFromBlockAuthTag for fromBlock: %d", fromBlk)
	}

	return fromBlk, nil
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

func (d *AuthDB) Has(key []byte) (bool, error) {
	return d.db.Has(key)
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

func (d *AuthDB) Put(key []byte, value []byte) error {
	return d.db.Put(key, value)
}

func (d *AuthDB) Stat() (string, error) {
	return d.db.Stat()
}

func (d *AuthDB) Get(key []byte) ([]byte, error) {
	// TODO: Intercepts the calls you care about
	return d.db.Get(key)
}
