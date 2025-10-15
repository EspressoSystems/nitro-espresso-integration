package authdb

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// The fields below define which low level database schema prefixes our AuthDB will intercept in Get

var (
	blockPrefix                   = []byte("blk-")
	blockAuthTagPrefix            = []byte("blkTag-")
	_headerPrefix                 = []byte("header-")
	headerAuthTagPrefix           = []byte("headerTag-")
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

func blockKey(blockNum uint64, blockHash common.Hash) []byte {
	return append(append(blockPrefix, EncodeUint64(blockNum)...), blockHash.Bytes()...)
}

func blockAuthTagKey(blockNum uint64, blockHash common.Hash) []byte {
	return append(append(blockAuthTagPrefix, EncodeUint64(blockNum)...), blockHash.Bytes()...)
}

// The `headerKey` name collides with rawdb.headerKey, so we need to use a different name.
func _headerKey(blockNum uint64, blockHash common.Hash) []byte {
	return append(append(_headerPrefix, EncodeUint64(blockNum)...), blockHash.Bytes()...)
}

func headerAuthTagKey(blockNum uint64, blockHash common.Hash) []byte {
	return append(append(headerAuthTagPrefix, EncodeUint64(blockNum)...), blockHash.Bytes()...)
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
