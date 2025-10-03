package arbnode

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/util/signature"
)

var BlockSignaturePrefix = []byte("blockSignature")

var (
	binarySearch_LessThanTarget    = -1
	binarySearch_GreaterThanTarget = 1
	binarySearch_EqualToTarget     = 0
)

// Looks for the first block number that is equal to or greater than the target
func binarySearchForBlockNumber(
	ctx context.Context,
	start, end uint64,
	f func(context.Context, uint64) (int, error),
) (uint64, error) {
	for start < end {
		mid := (start + end) / 2
		result, err := f(ctx, mid)
		if err != nil {
			return 0, err
		}
		if result == binarySearch_GreaterThanTarget {
			end = mid
		} else if result == binarySearch_LessThanTarget {
			start = mid + 1
		} else {
			// We are looking for the first block number.
			// So the loop should continue until start == end
			end = mid
		}
	}
	return start, nil
}

// We should be able to get the address as soon as we have the signer.
// We don't want to change a lot of code to make this work since we are working on a forked repo.
// This function is not costly and it should be called only once.
func recoverAddressFromSigner(signer signature.DataSignerFunc) (common.Address, error) {
	message := make([]byte, 32)
	signature, err := signer(message)
	if err != nil {
		return common.Address{}, err
	}

	publicKey, err := crypto.SigToPub(message, signature)
	if err != nil {
		return common.Address{}, err
	}

	return crypto.PubkeyToAddress(*publicKey), nil
}

// Create a signature over a uint64 value given a signer
func generateSignatureFromUint64(signer signature.DataSignerFunc, data uint64) ([]byte, error) {
	if signer == nil {
		return nil, nil
	}
	hash, err := getHashOverUint64(data)
	if err != nil {
		return nil, err
	}
	signature, err := signer(hash)
	if err != nil {
		return nil, err
	}
	return signature, nil
}

func generateSignatureOverHash(signer signature.DataSignerFunc, hash []byte) ([]byte, error) {
	if signer == nil {
		return nil, nil
	}

	signature, err := signer(hash)
	if err != nil {
		return nil, err
	}
	return signature, nil
}

func getHashOverUint64(data uint64) ([]byte, error) {
	uintBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(uintBytes, data)
	hash := crypto.Keccak256Hash(uintBytes)
	return hash.Bytes(), nil
}

func storeBlockSignature(batch ethdb.Batch, blockHash common.Hash, blockSignature []byte) error {
	blockNumber := binary.BigEndian.Uint64(blockHash.Bytes())
	key := dbKey(BlockSignaturePrefix, (blockNumber))
	return batch.Put(key, blockSignature)
}

func getBlockSignature(db ethdb.Database, blockHash common.Hash) ([]byte, error) {
	blockNumber := binary.BigEndian.Uint64(blockHash.Bytes())
	key := dbKey(BlockSignaturePrefix, (blockNumber))
	return db.Get(key)
}

func storeFromBlockWithSignature(batch ethdb.Batch, fromBlock uint64, fromBlockSignature []byte) error {

	blockNumberBytes, err := rlp.EncodeToBytes(fromBlock)
	if err != nil {
		return fmt.Errorf("failed to encode next from block: %w", err)
	}
	if err := batch.Put(DelayedFetcherCurrentFromBlockKey, blockNumberBytes); err != nil {
		return fmt.Errorf("failed to put from block: %w", err)
	}

	return batch.Put(DelayedFetcherCurrentFromBlockSignatureKey, fromBlockSignature)
}

func verifySignature(db ethdb.Database, signature []byte, hash []byte, snapshotSignerAddress common.Address) error {
	publicKeyBytes, err := crypto.Ecrecover(hash, signature)
	if err != nil {
		return fmt.Errorf("unable to recover public key")
	}
	pubKey, err := crypto.UnmarshalPubkey(publicKeyBytes)
	if err != nil || pubKey == nil {
		return fmt.Errorf("invalid public key")
	}
	// Public Key to address
	publicKeyAddress := crypto.PubkeyToAddress(*pubKey)
	// TODO: In follow up PRs, we should allows any valid PCR0 address registered in the contract
	// to be able to decrypt the snapshot

	if publicKeyAddress != snapshotSignerAddress {
		return fmt.Errorf("invalid snapshot address")
	}
	return nil
}
