package arbtest

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	hdwallet "github.com/miguelmota/go-ethereum-hdwallet"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbnode"
	"github.com/offchainlabs/nitro/espresso-tee-contracts/espressogen"
	testutils "github.com/offchainlabs/nitro/espresso/test-utils"
)

func (b *BlockchainTestInfo) GenerateAccountWithMnemonic(name string, mnemonic string, idx uint) error {
	if b.Accounts[name] != nil {
		b.T.Fatal("account already exists")
	}
	wallet, err := hdwallet.NewFromMnemonic(mnemonic)
	if err != nil {
		return err
	}
	path := hdwallet.MustParseDerivationPath(fmt.Sprintf("m/44'/60'/0'/0/%d", idx))
	account, err := wallet.Derive(path, false)
	if err != nil {
		return err
	}
	privateKey, err := wallet.PrivateKey(account)
	if err != nil {
		return err
	}

	b.Accounts[name] = &AccountInfo{
		Address:    account.Address,
		PrivateKey: privateKey,
		Nonce:      atomic.Uint64{},
	}
	log.Info("New Key ", "name", name, "Address", b.Accounts[name].Address)
	return nil
}

func createDummyEspressoMetadata(t *testing.T) []byte {
	return testutils.CreateDummyEspressoMetadata(t)
}

func deployMockTEEContracts(t *testing.T, transactionOpts *bind.TransactOpts, client *ethclient.Client) (common.Address, *types.Transaction, *espressogen.EspressoTEEVerifierMock, error) {
	ctx := transactionOpts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	nitro, nitroTx, contract, err := espressogen.DeployEspressoNitroTEEVerifierMock(transactionOpts, client)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to deploy EspressoNitroTEEVerifierMock: %w", err)
	}
	_, err = bind.WaitDeployed(ctx, client, nitroTx)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to confirm EspressoNitroTEEVerifierMock deployment: %w", err)
	}

	// Register the test key
	privKey := arbnode.TestEspressoPrivateKey
	signerAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	_, err = contract.RegisterService(transactionOpts, []byte{}, signerAddr.Bytes())

	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to register NTIRO test key: %w", err)
	}

	// _, err = contract.RegisterService(transactionOpts, []byte{}, signerAddr.Bytes(), 1)

	// if err != nil {
	// 	return common.Address{}, nil, nil, fmt.Errorf("failed to register Nitro test key: %w", err)
	// }

	return espressogen.DeployEspressoTEEVerifierMock(transactionOpts, client, nitro)
}
