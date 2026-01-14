package authdb

import (
	"bytes"
	"fmt"
	"sort"

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

func enforceAuthenticatedReader(db any) error {
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
			// Returning (nil, nil) is intentional: absence of events is not an error, but indicates no events have been stored yet.
			return nil, nil // nolint:nilerr
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

type AddrFlag struct {
	Addr common.Address
	Flag bool
}

type AddrFlagList []AddrFlag

func WriteAddresses(db ethdb.KeyValueWriter, addrs []map[common.Address]bool) error {
	if err := enforceAuthenticatedWriter(db); err != nil {
		return err
	}

	outer := make([]AddrFlagList, 0, len(addrs))

	for _, m := range addrs {
		list := make(AddrFlagList, 0, len(m))
		for addr, flag := range m {
			list = append(list, AddrFlag{
				Addr: addr,
				Flag: flag,
			})
		}

		sort.Slice(list, func(i, j int) bool {
			return bytes.Compare(
				list[i].Addr.Bytes(),
				list[j].Addr.Bytes(),
			) < 0
		})

		outer = append(outer, list)
	}

	// Encode as RLP
	encoded, err := rlp.EncodeToBytes(outer)
	if err != nil {
		return fmt.Errorf("failed to encode addresses: %w", err)
	}

	return db.Put(addressesKey, encoded)
}

func ReadAddresses(db ethdb.KeyValueReader) ([]map[common.Address]bool, error) {
	// Read raw bytes
	data, err := db.Get(addressesKey)
	if err != nil {
		if dbutil.IsErrNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	// Decode RLP -> [][]AddrFlag
	var outer []AddrFlagList
	if err := rlp.DecodeBytes(data, &outer); err != nil {
		return nil, fmt.Errorf("failed to decode addresses: %w", err)
	}

	// Convert back to []map[Address]bool
	result := make([]map[common.Address]bool, 0, len(outer))

	for _, list := range outer {
		m := make(map[common.Address]bool, len(list))
		for _, af := range list {
			m[af.Addr] = af.Flag
		}
		result = append(result, m)
	}

	return result, nil
}
