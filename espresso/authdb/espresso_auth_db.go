package authdb

import (
	"errors"

	"github.com/ethereum/go-ethereum/ethdb"
)

type AuthDB struct {
	db ethdb.KeyValueStore
}

func NewAuthDB(db ethdb.KeyValueStore) (AuthDB, error) {
	if db == nil {
		return AuthDB{}, errors.New("db is nil")
	}
	return AuthDB{db: db}, nil
}

func (d AuthDB) Close() error {
	return d.db.Close()
}

func (d AuthDB) Compact(start []byte, limit []byte) error {
	return d.db.Compact(start, limit)
}

func (d AuthDB) Delete(key []byte) error {
	return d.db.Delete(key)
}

func (d AuthDB) DeleteRange(start []byte, end []byte) error {
	return d.db.DeleteRange(start, end)
}

func (d AuthDB) Has(key []byte) (bool, error) {
	return d.db.Has(key)
}

func (d AuthDB) NewBatch() ethdb.Batch {
	return d.db.NewBatch()
}

func (d AuthDB) NewBatchWithSize(size int) ethdb.Batch {
	return d.db.NewBatchWithSize(size)
}

func (d AuthDB) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	return d.db.NewIterator(prefix, start)
}

func (d AuthDB) Put(key []byte, value []byte) error {
	return d.db.Put(key, value)
}

func (d AuthDB) Stat() (string, error) {
	return d.db.Stat()
}

func (d AuthDB) Get(key []byte) ([]byte, error) {
	// TODO: Intercepts the calls you care about
	return d.db.Get(key)
}
