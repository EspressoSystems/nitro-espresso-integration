package arbtest

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

func createDummyHotShotHeightAndSignature(t *testing.T) []byte {
	hotshotHeight := new(big.Int).SetUint64(1)
	signature := make([]byte, 32)

	uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		t.Fatal("failed to create uint256 type")
	}

	bytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		t.Fatal("failed to create bytes type")
	}

	hotshotNumberAndSignature, err := abi.Arguments{
		{Type: uint256Type},
		{Type: bytesType},
	}.Pack(hotshotHeight, signature)
	if err != nil {
		t.Fatal("failed to pack hotshot height and signature")
	}

	return hotshotNumberAndSignature
}
