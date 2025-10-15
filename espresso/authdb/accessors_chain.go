package authdb

import (
	"crypto/hmac"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

func (d *AuthDB) readHeader(hash common.Hash, number uint64) *types.Header {
	headerKey := _headerKey(number, hash)
	// Get the header bytes
	headerBytes, err := d.db.Get(headerKey)
	if err != nil {
		log.Error("Failed to get header bytes", "hash", hash, "err", err)
		return nil
	}

	// Contract block from headerBytes
	header := new(types.Header)
	err = rlp.DecodeBytes(headerBytes, header)
	if err != nil {
		log.Error("Failed to decode header", "hash", hash, "err", err)
		return nil
	}

	d.mac.Write(headerKey)
	d.mac.Write(headerBytes)
	taggedBlockHash := d.mac.Sum(nil)
	d.mac.Reset()

	if !hmac.Equal(taggedBlockHash, header.Hash().Bytes()) {
		log.Error("Block hash mismatch", "hash", hash, "number", number)
		return nil
	}

	if header.Number.Uint64() != number {
		log.Error("Block number mismatch", "hash", hash, "number", number)
		return nil
	}

	return header
}

func (d *AuthDB) readBody(hash common.Hash, number uint64) *types.Body {
	block := d.readBlock(hash, number)
	if block == nil {
		return nil
	}

	return block.Body()
}

func (d *AuthDB) readReceipts(hash common.Hash, number uint64) *types.Receipts {
	block := d.readBlock(hash, number)
	if block == nil {
		return nil
	}
	// TODO
	return nil
}

func (d *AuthDB) readBlock(hash common.Hash, number uint64) *types.Block {
	blockKey := blockKey(number, hash)
	// Get the block bytes
	blockBytes, err := d.db.Get(blockKey)
	if err != nil {
		log.Error("Failed to get block bytes", "number", number, "hash", hash, "err", err)
		return nil
	}

	if len(blockBytes) == 0 {
		log.Warn("Empty block bytes", "number", number, "hash", hash)
		return nil
	}

	// Contract block from blockBytes
	block := new(types.Block)
	err = rlp.DecodeBytes(blockBytes, block)
	if err != nil {
		log.Error("Failed to decode block", "number", number, "hash", hash, "err", err)
		return nil
	}

	d.mac.Write(blockKey)
	d.mac.Write(blockBytes)
	taggedBlockHash := d.mac.Sum(nil)
	d.mac.Reset()

	if !hmac.Equal(taggedBlockHash, block.Hash().Bytes()) {
		log.Error("Block hash mismatch", "number", number, "hash", hash, "blockHash", hash)
		return nil
	}

	if block.Header().Number.Uint64() != number {
		log.Error("Block number mismatch", "number", number, "hash", hash, "number", number)
		return nil
	}
	return block
}

func (d *AuthDB) readHeaderHash(hash common.Hash, number uint64) common.Hash {
	header := d.readHeader(hash, number)
	if header == nil {
		return common.Hash{}
	}
	return header.Hash()
}
