package authdb

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"

	espresso_tee_utils "github.com/offchainlabs/nitro/cmd/util/espresso-tee-utils"
)

// make sure that CaffNode-specific operations that require authenticated DBs fail when given a plain reader/writer
func TestAuthCaffNodeOperations(t *testing.T) {
	// Create a plain memorydb (not AuthDB)
	plainDB := rawdb.NewDatabase(memorydb.New())
	defer plainDB.Close()

	// Test writes with plain DB - should fail
	err := WriteNextHotshotBlockNum(plainDB, 123)
	Assert(t, err != nil, "expected error when writing NextHotshotBlockNum to plain db, but got nil")

	err = WriteEvents(plainDB, []byte{0x01, 0x02, 0x03})
	Assert(t, err != nil, "expected error when writing Events to plain db, but got nil")

	err = WriteFromBlock(plainDB, 456)
	Assert(t, err != nil, "expected error when writing FromBlock to plain db, but got nil")

	err = WriteLastProcessedHeight(plainDB, 789)
	Assert(t, err != nil, "expected error when writing LastProcessedHeight to plain db, but got nil")

	// Test reads with plain DB - should fail
	_, err = ReadNextHotshotBlockNum(plainDB)
	Assert(t, err != nil, "expected error when reading NextHotshotBlockNum from plain db, but got nil")

	_, err = ReadEvents(plainDB)
	Assert(t, err != nil, "expected error when reading Events from plain db, but got nil")

	_, err = ReadFromBlock(plainDB)
	Assert(t, err != nil, "expected error when reading FromBlock from plain db, but got nil")

	_, err = ReadLastProcessedHeight(plainDB)
	Assert(t, err != nil, "expected error when reading LastProcessedHeight from plain db, but got nil")

	// Test WriteAddresses with plain DB - should fail
	plainAddrs := []map[common.Address]bool{
		{
			common.HexToAddress("0x1000000000000000000000000000000000000001"): true,
		},
	}
	err = WriteAddresses(plainDB, plainAddrs)
	Assert(t, err != nil, "expected error when writing addresses to plain db, but got nil")

	// Test with plain batch - should fail
	plainBatch := plainDB.NewBatch()

	err = WriteNextHotshotBlockNum(plainBatch, 123)
	Assert(t, err != nil, "expected error when writing NextHotshotBlockNum to plain batch, but got nil")

	err = WriteEvents(plainBatch, []byte{0x01})
	Assert(t, err != nil, "expected error when writing Events to plain batch, but got nil")

	err = WriteFromBlock(plainBatch, 456)
	Assert(t, err != nil, "expected error when writing FromBlock to plain batch, but got nil")

	err = WriteLastProcessedHeight(plainBatch, 789)
	Assert(t, err != nil, "expected error when writing LastProcessedHeight to plain batch, but got nil")

	// Test WriteAddresses with plain batch - should fail
	err = WriteAddresses(plainBatch, plainAddrs)
	Assert(t, err != nil, "expected error when writing addresses to plain batch, but got nil")

	hmac, err := espresso_tee_utils.HmacForTest()
	Require(t, err)
	authdb, err := NewAuthDB(plainDB, hmac, false)
	Require(t, err)
	defer authdb.Close()

	// Test with AuthDB
	err = WriteNextHotshotBlockNum(&authdb, 123)
	Require(t, err)
	value, err := ReadNextHotshotBlockNum(&authdb)
	Require(t, err)
	if value != 123 {
		t.Fatalf("expected value 123, got %d", value)
	}

	err = WriteEvents(&authdb, []byte{0x01, 0x02, 0x03})
	Require(t, err)
	_, err = ReadEvents(&authdb)
	Require(t, err)

	// Test with AuthBatch
	batch := authdb.NewBatch()

	err = WriteFromBlock(batch, 456)
	Require(t, err)
	err = WriteLastProcessedHeight(batch, 789)
	Require(t, err)
	err = batch.Write()
	Require(t, err)

	fromBlk, err := ReadFromBlock(&authdb)
	Require(t, err)
	if fromBlk != 456 {
		t.Fatalf("expected fromBlk 456, got %d", fromBlk)
	}

	lastBlk, err := ReadLastProcessedHeight(&authdb)
	Require(t, err)
	if lastBlk != 789 {
		t.Fatalf("expected lastBlk 789, got %d", lastBlk)
	}

	// Test WriteAddresses / ReadAddresses with AuthDB and AuthBatch
	a1 := common.HexToAddress("0x2000000000000000000000000000000000000001")
	a2 := common.HexToAddress("0x2000000000000000000000000000000000000002")
	a3 := common.HexToAddress("0x2000000000000000000000000000000000000003")

	authAddrs := []map[common.Address]bool{
		{
			a1: true,
			a2: false,
		},
		{
			a3: true,
		},
	}

	// Before writing, ReadAddresses on AuthDB should return nil, nil (no data)
	addrRes, err := ReadAddresses(&authdb)
	Require(t, err)
	if addrRes != nil {
		t.Fatalf("expected nil addresses before write, got %v", addrRes)
	}

	// Write using AuthDB
	err = WriteAddresses(&authdb, authAddrs)
	Require(t, err)

	// Read back and compare
	addrRes, err = ReadAddresses(&authdb)
	Require(t, err)
	if len(addrRes) != len(authAddrs) {
		t.Fatalf("expected %d address lists, got %d", len(authAddrs), len(addrRes))
	}
	for i, expectedMap := range authAddrs {
		gotMap := addrRes[i]
		if len(gotMap) != len(expectedMap) {
			t.Fatalf("index %d: expected %d addrs, got %d", i, len(expectedMap), len(gotMap))
		}
		for addr, flag := range expectedMap {
			gotFlag, ok := gotMap[addr]
			if !ok {
				t.Fatalf("index %d: missing addr %s", i, addr.Hex())
			}
			if gotFlag != flag {
				t.Fatalf("index %d: addr %s flag mismatch: expected %v, got %v", i, addr.Hex(), flag, gotFlag)
			}
		}
	}

	// Now test writing via AuthBatch
	addrBatch := authdb.NewBatch()
	err = WriteAddresses(addrBatch, authAddrs)
	Require(t, err)
	err = addrBatch.Write()
	Require(t, err)

	addrRes, err = ReadAddresses(&authdb)
	Require(t, err)
	if len(addrRes) != len(authAddrs) {
		t.Fatalf("(batch) expected %d address lists, got %d", len(authAddrs), len(addrRes))
	}
	for i, expectedMap := range authAddrs {
		gotMap := addrRes[i]
		if len(gotMap) != len(expectedMap) {
			t.Fatalf("(batch) index %d: expected %d addrs, got %d", i, len(expectedMap), len(gotMap))
		}
		for addr, flag := range expectedMap {
			gotFlag, ok := gotMap[addr]
			if !ok {
				t.Fatalf("(batch) index %d: missing addr %s", i, addr.Hex())
			}
			if gotFlag != flag {
				t.Fatalf("(batch) index %d: addr %s flag mismatch: expected %v, got %v", i, addr.Hex(), flag, gotFlag)
			}
		}
	}
}
