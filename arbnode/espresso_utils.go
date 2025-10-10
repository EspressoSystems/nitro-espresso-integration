package arbnode

import (
	"context"
	"fmt"
	"hash"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/espresso/authdb"
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

func generateSignatureOverBlock(signer hash.Hash, block *types.Block) ([]byte, error) {
	if block == nil {
		return nil, nil
	}

	blockBytes, err := rlp.EncodeToBytes(block)
	if err != nil {
		return nil, err
	}
	signer.Write(blockBytes)
	signature := signer.Sum(nil)
	return signature, nil
}

func storeBlockSignature(batch ethdb.Batch, blockHash common.Hash, blockSignature []byte) error {
	return batch.Put(authdb.BlockSignatureKey(blockHash), blockSignature)
}

func getBlockSignature(db authdb.AuthDB, blockHash common.Hash) ([]byte, error) {
	return db.Get(authdb.BlockSignatureKey(blockHash))
}
