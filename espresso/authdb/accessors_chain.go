package authdb

import (
	"crypto/hmac"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

func (d *AuthDB) authReadHeader(hash common.Hash, number uint64) ([]byte, error) {
	headerKey := headerKey(number, hash)
	headerBytes, err := d.db.Get(headerKey)
	if err != nil {
		log.Error("Failed to get header bytes", "hash", hash, "err", err)
		return nil, err
	}

	header := new(types.Header)
	err = rlp.DecodeBytes(headerBytes, header)
	if err != nil {
		log.Error("Failed to decode header", "hash", hash, "err", err)
		return nil, err
	}

	expectedTag, err := d.db.Get(headerAuthTagKey(number, hash))
	if err != nil {
		log.Error("Failed to get header auth tag", "hash", hash, "err", err)
		return nil, err
	}

	d.mac.Write(headerKey)
	d.mac.Write(headerBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()

	if !hmac.Equal(tag, expectedTag) {
		log.Error("failed to authenticate header", "hash", hash, "number", number)
		return nil, fmt.Errorf("failed to authenticate header, hash: %v, number: %d", hash, number)
	}

	return headerBytes, nil
}

func (d *AuthDB) authReadBody(hash common.Hash, number uint64) ([]byte, error) {
	bodyKey := blockBodyKey(number, hash)
	bodyBytes, err := d.db.Get(bodyKey)
	if err != nil {
		log.Error("Failed to get body bytes", "hash", hash, "err", err)
		return nil, err
	}

	body := new(types.Body)
	err = rlp.DecodeBytes(bodyBytes, body)
	if err != nil {
		log.Error("Failed to decode body", "hash", hash, "err", err)
		return nil, err
	}

	expectedTag, err := d.db.Get(bodyAuthTagKey(number, hash))
	if err != nil {
		log.Error("Failed to get body auth tag", "hash", hash, "err", err)
		return nil, err
	}

	d.mac.Write(bodyKey)
	d.mac.Write(bodyBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()

	if !hmac.Equal(tag, expectedTag) {
		log.Error("failed to authenticate body", "hash", hash, "number", number)
		return nil, fmt.Errorf("failed to authenticate body, hash: %v, number: %d", hash, number)
	}

	return bodyBytes, nil
}

func (d *AuthDB) authReadReceipts(hash common.Hash, number uint64) ([]byte, error) {
	receiptsKey := blockBodyKey(number, hash)
	receiptsBytes, err := d.db.Get(receiptsKey)
	if err != nil {
		log.Error("Failed to get receipts bytes", "hash", hash, "err", err)
		return nil, err
	}

	receipts := new(types.Receipts)
	err = rlp.DecodeBytes(receiptsBytes, receipts)
	if err != nil {
		log.Error("Failed to decode receipts", "hash", hash, "err", err)
		return nil, err
	}

	expectedTag, err := d.db.Get(receiptsAuthTagKey(number, hash))
	if err != nil {
		log.Error("Failed to get receipts auth tag", "hash", hash, "err", err)
		return nil, err
	}

	d.mac.Write(receiptsKey)
	d.mac.Write(receiptsBytes)
	tag := d.mac.Sum(nil)
	d.mac.Reset()

	if !hmac.Equal(tag, expectedTag) {
		log.Error("failed to authenticate receipts", "hash", hash, "number", number)
		return nil, fmt.Errorf("failed to authenticate receipts, hash: %v, number: %d", hash, number)
	}

	return receiptsBytes, nil
}

// func (d *AuthDB) authReadBlock(hash common.Hash, number uint64) ([]byte, error) {
// 	bodyBytes, err := d.authReadBody(hash, number)
// 	if err != nil {
// 		return nil, err
// 	}
// 	body := new(types.Body)
// 	if err := rlp.DecodeBytes(bodyBytes, body); err != nil {
// 		return nil, fmt.Errorf("failed to decode body: %w", err)
// 	}

// 	headerBytes, err := d.authReadHeader(hash, number)
// 	if err != nil {
// 		return nil, err
// 	}
// 	header := new(types.Header)
// 	if err := rlp.DecodeBytes(headerBytes, header); err != nil {
// 		return nil, fmt.Errorf("failed to decode header: %w", err)
// 	}

// 	block := types.NewBlockWithHeader(header).WithBody(*body)
// 	return rlp., nil
// }
