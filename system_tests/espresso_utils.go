package arbtest

import (
	"fmt"
	"sync/atomic"
	"testing"

	hdwallet "github.com/miguelmota/go-ethereum-hdwallet"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"

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
	sgx, _, _, err := espressogen.DeployEspressoSGXTEEVerifierMock(transactionOpts, client)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to deploy EspressoSGXTEEVerifierMock: %w", err)
	}
	log.Info("sgx address", "sgx", sgx)

	nitro, _, _, err := espressogen.DeployEspressoNitroTEEVerifierMock(transactionOpts, client)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to deploy EspressoNitroTEEVerifierMock: %w", err)
	}
	log.Info("nitro address", "nitro", nitro)

	return espressogen.DeployEspressoTEEVerifierMock(transactionOpts, client, sgx, nitro)
}
