package arbtest

import (
	"math/big"
	"testing"

	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/offchainlabs/nitro/arbnode"
	"github.com/offchainlabs/nitro/espresso-tee-contracts/espressogen"
)

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

func createDummyEspressoMetadata(
	t *testing.T,
	seqNum *big.Int,
	message []byte,
	afterDelayedMsgRead *big.Int,
	gasRefunder common.Address,
	prevMessageCount *big.Int,
	newMessageCount *big.Int,
) []byte {
	// SGX by default
	teeType := uint8(0)
	hotshotHeight := new(big.Int).SetUint64(1)

	uint256Type, _ := abi.NewType("uint256", "", nil)
	bytesType, _ := abi.NewType("bytes", "", nil)
	uint8Type, _ := abi.NewType("uint8", "", nil)
	addressType, _ := abi.NewType("address", "", nil)

	// abi.encode(
	//   sequenceNumber, data, afterDelayedMessagesRead, address(gasRefunder), prevMessageCount, newMessageCount, hotshotHeight)
	packed, err := abi.Arguments{
		{Type: uint256Type}, // sequenceNumber
		{Type: bytesType},   // data
		{Type: uint256Type}, // afterDelayedMessagesRead
		{Type: addressType}, // gasRefunder
		{Type: uint256Type}, // prevMessageCount
		{Type: uint256Type}, // newMessageCount
		{Type: uint256Type}, // hotshotHeight
	}.Pack(seqNum, message, afterDelayedMsgRead, gasRefunder, prevMessageCount, newMessageCount, hotshotHeight)
	if err != nil {
		t.Fatal("failed to abi.encode reportDataHash params: ", err)
	}
	reportDataHash := crypto.Keccak256Hash(packed)

	signature, err := crypto.Sign(reportDataHash.Bytes(), arbnode.TestEspressoPrivateKey)
	if err != nil {
		t.Fatal("failed to sign reportDataHash")
	}
	if len(signature) != 65 {
		t.Fatalf("signature length is not 65 bytes, got %d", len(signature))
	}
	if signature[64] == 0 || signature[64] == 1 {
		signature[64] += 27
	}

	espressoMetadata, err := abi.Arguments{
		{Type: uint256Type},
		{Type: bytesType},
		{Type: uint8Type},
	}.Pack(hotshotHeight, signature, teeType)
	if err != nil {
		t.Fatal("failed to pack espresso metadata")
	}

	return espressoMetadata
}
