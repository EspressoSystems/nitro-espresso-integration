package authdb

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// The fields below define which low level database schema prefixes our AuthDB will intercept in Get

var (
	// authenticated Geth
	headerAuthTagPrefix   = []byte("headerTag-")
	bodyAuthTagPrefix     = []byte("bodyTag-")
	receiptsAuthTagPrefix = []byte("receiptsTag-")

	// caff node specific
	fromBlockKey                  = []byte("fromBlk")
	fromBlockAuthTagKey           = []byte("fromBlkTag")
	nextHotshotBlockNumKey        = []byte("nextHsBlkNum")
	nextHotshotBlockNumAuthTagKey = []byte("nextHsBlkNumTag")
	initAddressesKey              = []byte("initAddrs")
	initAddressesAuthTagKey       = []byte("initAddrsTag")
	eventsKey                     = []byte("events")
	eventsAuthTagKey              = []byte("eventsTag")
	lastProcessedHeightKey        = []byte("lastProcessedHeight")
	lastProcessedHeightAuthTagKey = []byte("lastProcessedHeightTag")
)

func bodyAuthTagKey(blockNum uint64, blockHash common.Hash) []byte {
	return append(append(bodyAuthTagPrefix, EncodeUint64(blockNum)...), blockHash.Bytes()...)
}

func headerAuthTagKey(blockNum uint64, blockHash common.Hash) []byte {
	return append(append(headerAuthTagPrefix, EncodeUint64(blockNum)...), blockHash.Bytes()...)
}

func receiptsAuthTagKey(blockNum uint64, blockHash common.Hash) []byte {
	return append(append(receiptsAuthTagPrefix, EncodeUint64(blockNum)...), blockHash.Bytes()...)
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

// helper func to parse db-key with pattern:
// prefix + num (uint64 big endian) + hash
func parseUint64AndHash(key []byte, prefixLen int) (uint64, common.Hash, error) {
	var hash common.Hash
	expectedKeyLen := prefixLen + 8 + common.HashLength
	if len(key) != expectedKeyLen {
		return 0, hash, fmt.Errorf("expected key len: %d, got: %d", expectedKeyLen, len(key))
	}

	number, err := DecodeUint64(key[prefixLen : prefixLen+8])
	if err != nil {
		log.Error("failed to parse block number from db key", "err", err)
		return 0, hash, err
	}

	hash = common.BytesToHash(key[prefixLen+8 : prefixLen+8+common.HashLength])
	return number, hash, nil
}
