package arbtest

import (
	"context"
	"crypto/ecdsa"
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
	"github.com/offchainlabs/nitro/espressotee"
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

	// Register the test key
	privKey := arbnode.TestEspressoPrivateKey

	nitro, nitroTx, nitroMock, err := espressogen.DeployEspressoNitroTEEVerifierMock(transactionOpts, client)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to deploy EspressoNitroTEEVerifierMock: %w", err)
	}
	_, err = bind.WaitDeployed(ctx, client, nitroTx)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to confirm EspressoNitroTEEVerifierMock deployment: %w", err)
	}

	err = registerNitroMockBatchPosterTestKey(transactionOpts, nitroMock, privKey)
	if err != nil {
		return common.Address{}, nil, nil, err
	}


	return espressogen.DeployEspressoTEEVerifierMock(transactionOpts, client, nitro)
}

func registerNitroMockBatchPosterTestKey(
	transactionOpts *bind.TransactOpts,
	nitroMock *espressogen.EspressoNitroTEEVerifierMock,
	privKey *ecdsa.PrivateKey,
) error {
	journalBytes, err := encodeNitroMockBatchPosterVerifierJournalPublicKey(crypto.FromECDSAPub(&privKey.PublicKey))
	if err != nil {
		return fmt.Errorf("failed to encode Nitro mock VerifierJournal for batch poster: %w", err)
	}
	_, err = nitroMock.RegisterService(transactionOpts, journalBytes, []byte{})
	if err != nil {
		return fmt.Errorf("failed to register Nitro mock batch poster test key: %w", err)
	}
	return nil
}

func encodeNitroMockBatchPosterVerifierJournalPublicKey(publicKey []byte) ([]byte, error) {
	return espressotee.EncodeNitroMockVerifierJournalPublicKey(publicKey)
}
