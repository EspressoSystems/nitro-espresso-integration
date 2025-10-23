package authdb

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/core/rawdb"
)

// The fields below define which low level database schema prefixes our AuthDB will intercept in Get

var (
	// authenticated Geth
	genericAuthTagSuffix = []byte("-tag")

	// caff node specific
	fromBlockKey           = []byte("fromBlk")
	nextHotshotBlockNumKey = []byte("nextHsBlkNum")
	initAddressesKey       = []byte("initAddrs")
	eventsKey              = []byte("events")
	lastProcessedHeightKey = []byte("lastProcessedHeight")
)

// Tag Freezer Configuration
// The tag freezer stores HMAC authentication tags in a separate ancient store
// parallel to the main chain freezer. Each tag table corresponds to a main
// ancient table and stores raw HMAC bytes indexed by the same item number.
const (
	// AuthTagFreezerName is the subfolder name for the tag ancient store
	AuthTagFreezerName = "auth-tags"

	// Tag table names - one per main ancient table
	// These store raw HMAC tags corresponding to items in the main tables
	AuthTagHashTable    = "tag-hashes"   // Tags for ChainFreezerHashTable
	AuthTagHeaderTable  = "tag-headers"  // Tags for ChainFreezerHeaderTable
	AuthTagBodiesTable  = "tag-bodies"   // Tags for ChainFreezerBodiesTable
	AuthTagReceiptTable = "tag-receipts" // Tags for ChainFreezerReceiptTable
)

// authTagTableNoSnappy configures compression for tag tables.
// Tags are random HMAC outputs that don't compress well.
var authTagTableNoSnappy = map[string]bool{
	AuthTagHashTable:    true,
	AuthTagHeaderTable:  true,
	AuthTagBodiesTable:  true,
	AuthTagReceiptTable: true,
}

// freezerTabletoTagTable maps main ancient table names to their corresponding tag table names
var freezerTabletoTagTable = map[string]string{
	rawdb.ChainFreezerHashTable:    AuthTagHashTable,
	rawdb.ChainFreezerHeaderTable:  AuthTagHeaderTable,
	rawdb.ChainFreezerBodiesTable:  AuthTagBodiesTable,
	rawdb.ChainFreezerReceiptTable: AuthTagReceiptTable,
}

// getTagTable returns the tag table name for a given main ancient table kind
func getTagTable(kind string) (string, bool) {
	tagTable, ok := freezerTabletoTagTable[kind]
	return tagTable, ok
}

func EncodeUint64(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

func DecodeUint64(enc []byte) (uint64, error) {
	if len(enc) != 8 {
		return 0, fmt.Errorf("invalid length")
	}
	var number uint64
	err := binary.Read(bytes.NewReader(enc), binary.BigEndian, &number)
	return number, err
}

func genericAuthTagKey(key []byte) []byte {
	return append(key, genericAuthTagSuffix...)
}
