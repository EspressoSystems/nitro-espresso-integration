package arbnode

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/espressotee"
	"github.com/offchainlabs/nitro/solgen/go/espressogen"
	"github.com/offchainlabs/nitro/util/signature"
)

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
		fmt.Println("mid", mid)
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

func setupNitroVerifier(teeVerifier *espressogen.IEspressoTEEVerifier, l1Client *ethclient.Client) (espressotee.EspressoNitroTEEVerifierInterface, error) {
	// Setup nitro contract interface
	nitroAddr, err := teeVerifier.EspressoNitroTEEVerifier(&bind.CallOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to get nitro tee verifier address from caller: %v", err)
	}
	log.Info("succesfully retrieved nitro contract verifier address", "address", nitroAddr)

	nitroVerifierBindings, err := espressogen.NewIEspressoNitroTEEVerifier(
		nitroAddr,
		l1Client)
	if err != nil {
		return nil, err
	}
	nitroVerifier := espressotee.NewEspressoNitroTEEVerifier(nitroVerifierBindings, l1Client)
	return nitroVerifier, nil
}

func getMessageForSubmittingToEspresso(
	pos arbutil.MessageIndex,
	fetcher func(pos arbutil.MessageIndex) (*arbostypes.MessageWithMetadata, error)) (*arbostypes.MessageWithMetadata, error) {
	msg, err := fetcher(pos)
	if err != nil {
		return nil, err
	}
	if pos >= 1 {
		prevMsg, err := fetcher(pos - 1)
		if err != nil {
			return nil, err
		}
		if prevMsg.DelayedMessagesRead+1 == msg.DelayedMessagesRead {
			// This message is a delayed message, and it should not be included
			// in the hotshot payload. The caff node is supposed to fetch the delayed message
			// from L1.
			// setting `msg.Message` to `nil` will cause a rlp decode/encode error
			// so we set `L2msg` to an empty byte slice instead
			msg.Message.L2msg = []byte{}
		}
	}
	return msg, nil
}
