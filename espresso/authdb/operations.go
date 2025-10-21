package authdb

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/util/dbutil"
)

// enforceAuthenticatedDB ensures that the db parameter is either *AuthDB or *AuthBatch
// to guarantee that Put/Get operations are authenticated
func enforceAuthenticatedWriter(db ethdb.KeyValueWriter) error {
	switch db.(type) {
	case *AuthDB, *AuthBatch:
		return nil
	default:
		return fmt.Errorf("db must be *AuthDB or *AuthBatch to ensure authenticated operations, got %T", db)
	}
}

func enforceAuthenticatedReader(db ethdb.KeyValueReader) error {
	switch db.(type) {
	case *AuthDB, *AuthBatch:
		return nil
	default:
		return fmt.Errorf("db must be *AuthDB or *AuthBatch to ensure authenticated operations, got %T", db)
	}
}

// WriteNextHotshotBlockNum writes the next hotshot block number
func WriteNextHotshotBlockNum(db ethdb.KeyValueWriter, num uint64) error {
	if err := enforceAuthenticatedWriter(db); err != nil {
		return err
	}
	return db.Put(nextHotshotBlockNumKey, EncodeUint64(num))
}

// ReadNextHotshotBlockNum reads the next hotshot block number
func ReadNextHotshotBlockNum(db ethdb.KeyValueReader) (uint64, error) {
	if err := enforceAuthenticatedReader(db); err != nil {
		return 0, err
	}
	numBytes, err := db.Get(nextHotshotBlockNumKey)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get nextHotshotBlockNum: %w", err)
	}
	return DecodeUint64(numBytes)
}

// WriteFromBlock writes the delayed message fetcher's FromBlock info
func WriteFromBlock(db ethdb.KeyValueWriter, fromBlk uint64) error {
	if err := enforceAuthenticatedWriter(db); err != nil {
		return err
	}
	return db.Put(fromBlockKey, EncodeUint64(fromBlk))
}

// ReadFromBlock reads the delayed message fetcher's FromBlock info
func ReadFromBlock(db ethdb.KeyValueReader) (uint64, error) {
	if err := enforceAuthenticatedReader(db); err != nil {
		return 0, err
	}
	numBytes, err := db.Get(fromBlockKey)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get fromBlock: %w", err)
	}
	return DecodeUint64(numBytes)
}

// WriteInitAddresses writes the batcher address monitor's InitAddresses info
func WriteInitAddresses(db ethdb.KeyValueWriter, addrs []common.Address) error {
	if err := enforceAuthenticatedWriter(db); err != nil {
		return err
	}
	if len(addrs) == 0 {
		return nil
	}
	addrsBytes, err := rlp.EncodeToBytes(addrs)
	if err != nil {
		return fmt.Errorf("failed to encode addrs: %w", err)
	}
	return db.Put(initAddressesKey, addrsBytes)
}

// ReadInitAddresses reads the batcher address monitor's InitAddresses info
func ReadInitAddresses(db ethdb.KeyValueReader) ([]common.Address, error) {
	if err := enforceAuthenticatedReader(db); err != nil {
		return nil, err
	}
	addrsBytes, err := db.Get(initAddressesKey)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			// nolint:nilerr
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get init addrs: %w", err)
	}

	var addrs []common.Address
	err = rlp.DecodeBytes(addrsBytes, &addrs)
	if err != nil {
		return nil, fmt.Errorf("failed to decode addrs: %w", err)
	}
	return addrs, nil
}

// WriteEvents writes the batcher address monitor's Events info
// We accept RLP-encoded events to avoid cyclic dependency since `BatcherAddrUpdate struct`
// is defined in `arbnode` which will depend on this function
func WriteEvents(db ethdb.KeyValueWriter, eventsBytes []byte) error {
	if err := enforceAuthenticatedWriter(db); err != nil {
		return err
	}
	return db.Put(eventsKey, eventsBytes)
}

// ReadEvents reads the batcher address monitor's Events info
func ReadEvents(db ethdb.KeyValueReader) ([]byte, error) {
	if err := enforceAuthenticatedReader(db); err != nil {
		return nil, err
	}
	eventsBytes, err := db.Get(eventsKey)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			// nolint:nilerr
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get events: %w", err)
	}
	return eventsBytes, nil
}

// WriteLastProcessedHeight writes the batcher address monitor's LastProcessedHeight info
func WriteLastProcessedHeight(db ethdb.KeyValueWriter, height uint64) error {
	if err := enforceAuthenticatedWriter(db); err != nil {
		return err
	}
	return db.Put(lastProcessedHeightKey, EncodeUint64(height))
}

// ReadLastProcessedHeight reads the batcher address monitor's LastProcessedHeight info
func ReadLastProcessedHeight(db ethdb.KeyValueReader) (uint64, error) {
	if err := enforceAuthenticatedReader(db); err != nil {
		return 0, err
	}
	heightBytes, err := db.Get(lastProcessedHeightKey)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get last processed height: %w", err)
	}
	return DecodeUint64(heightBytes)
}
