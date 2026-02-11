package arbtest

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/offchainlabs/nitro/arbnode"
	"github.com/offchainlabs/nitro/espresso-tee-contracts/espressogen"
)

func createDummyEspressoMetadata(t *testing.T) []byte {
	hotshotHeight := new(big.Int).SetUint64(1)
	signature := make([]byte, 32)
	teeType := uint8(0)

	uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		t.Fatal("failed to create uint256 type")
	}

	bytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		t.Fatal("failed to create bytes type")
	}

	uint8Type, err := abi.NewType("uint8", "", nil)
	if err != nil {
		t.Fatal("failed to create uint8 type")
	}

	espressoMetadata, err := abi.Arguments{
		{Type: uint256Type},
		{Type: bytesType},
		{Type: uint8Type},
	}.Pack(hotshotHeight, signature, teeType)
	if err != nil {
		t.Fatal("failed to pack hotshot height and signature")
	}

	return espressoMetadata
}

func deployMockTEEContracts(t *testing.T, transactionOpts *bind.TransactOpts, client *ethclient.Client) (common.Address, *types.Transaction, *espressogen.EspressoTEEVerifierMock, error) {
	ctx := transactionOpts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	sgx, sgxTx, contract, err := espressogen.DeployEspressoSGXTEEVerifierMock(transactionOpts, client)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to deploy EspressoSGXTEEVerifierMock: %w", err)
	}
	_, err = bind.WaitDeployed(ctx, client, sgxTx)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to confirm EspressoSGXTEEVerifierMock deployment: %w", err)
	}

	// Register the test key
	privKey := arbnode.TestEspressoPrivateKey
	signerAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	_, err = contract.RegisterService(transactionOpts, []byte{}, signerAddr.Bytes(), 0)

	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to register SGX test key: %w", err)
	}

	_, err = contract.RegisterService(transactionOpts, []byte{}, signerAddr.Bytes(), 1)

	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to register Nitro test key: %w", err)
	}

	nitro, _, _, err := espressogen.DeployEspressoNitroTEEVerifierMock(transactionOpts, client)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to deploy EspressoNitroTEEVerifierMock: %w", err)
	}

	return espressogen.DeployEspressoTEEVerifierMock(transactionOpts, client, sgx, nitro)
}
