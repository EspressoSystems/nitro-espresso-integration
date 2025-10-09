package authdb

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// The fields below define which low level database schema prefixes our AuthDB will intercept in Get

var (
	BlockSignaturePrefix                          = []byte("blockSignature")
	DelayedMessageFetcherFromBlockPrefix          = []byte("delayedFetcherFromBlock")
	DelayedMessageFetcherFromBlockSignaturePrefix = []byte("delayedFetcherFromBlockSignature")
	StreamerHotshotBlockPrefix                    = []byte("streamerHotshotBlock")
	StreamerHotshotBlockSignaturePrefix           = []byte("streamerHotshotBlockSignature")
)

func BlockSignatureKey(blockHash common.Hash) []byte {
	return append(BlockSignaturePrefix, blockHash.Bytes()...)
}

func DelayedMessageFetcherFromBlockKey(blockNum uint64) []byte {
	return append(DelayedMessageFetcherFromBlockPrefix, EncodeUint64(blockNum)...)
}

func DelayedMessageFetcherFromBlockSignatureKey(blockNum uint64) []byte {
	return append(DelayedMessageFetcherFromBlockSignaturePrefix, EncodeUint64(blockNum)...)
}

func StreamerHotshotBlockKey(blockNum uint64) []byte {
	return append(StreamerHotshotBlockPrefix, EncodeUint64(blockNum)...)
}

func StreamerHotshotBlockSignatureKey(blockNum uint64) []byte {
	return append(StreamerHotshotBlockSignaturePrefix, EncodeUint64(blockNum)...)
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
