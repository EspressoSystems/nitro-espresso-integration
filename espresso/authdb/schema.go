package authdb

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// The fields below define which low level database schema prefixes our AuthDB will intercept in Get

var (
	// authenticated Geth
	// genericAuthTagPrefix = []byte("tag-")
	genericAuthTagSuffix = []byte("-tag")

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
