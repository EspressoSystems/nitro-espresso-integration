package authdb

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// The fields below define which low level database schema prefixes our AuthDB will intercept in Get

const (
	BlockSignaturePrefix                        = []byte("blkSig-")
	DelayedMessageFetcherFromBlockKey           = []byte("delayedFetcherFromBlk")
	DelayedMessageFetcherFromBlockAuthTagPrefix = []byte("delayedFetcherFromBlkTag-")
	NextHotshotBlockNumKey                      = []byte("nextHsBlkNum")
	NextHotshotBlockNumAuthTagPrefix            = []byte("nextHsBlkNumTag-")
)

func BlockSignatureKey(blockHash common.Hash) []byte {
	return append(BlockSignaturePrefix, blockHash.Bytes()...)
}

func DelayedMessageFetcherFromBlockAuthTagKey(blockNum uint64) []byte {
	return append(DelayedMessageFetcherFromBlockAuthTagPrefix, EncodeUint64(blockNum)...)
}

func NextHotshotBlockNumAuthTagKey(blockNum uint64) []byte {
	return append(NextHotshotBlockNumAuthTagPrefix, EncodeUint64(blockNum)...)
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
