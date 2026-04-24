package arbtest

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	hdwallet "github.com/miguelmota/go-ethereum-hdwallet"

	"github.com/ethereum/go-ethereum/accounts/abi"
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

	nitro, nitroTx, nitroMock, err := espressogen.DeployEspressoNitroTEEVerifierMock(transactionOpts, client)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to deploy EspressoNitroTEEVerifierMock: %w", err)
	}
	_, err = bind.WaitDeployed(ctx, client, nitroTx)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to confirm EspressoNitroTEEVerifierMock deployment: %w", err)
	}

	// Pre-register for TESTS→NITRO key-manager mode.
	journalBytes, err := encodeVerifierJournalPublicKey(crypto.FromECDSAPub(&privKey.PublicKey))
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to encode VerifierJournal: %w", err)
	}
	_, err = nitroMock.RegisterService(transactionOpts, journalBytes, []byte{}, 0)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to register Nitro BatchPoster test key: %w", err)
	}
	_, err = nitroMock.RegisterService(transactionOpts, journalBytes, []byte{}, 1)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("failed to register Nitro CaffNode test key: %w", err)
	}

	return espressogen.DeployEspressoTEEVerifierMock(transactionOpts, client, sgx, nitro)
}

// ABI-encoded VerifierJournal with only PublicKey set; NITRO mock ignores the rest.
func encodeVerifierJournalPublicKey(publicKey []byte) ([]byte, error) {
	journalType, err := abi.NewType("tuple", "VerifierJournal", []abi.ArgumentMarshaling{
		{Name: "result", Type: "uint8"},
		{Name: "trustedCertsPrefixLen", Type: "uint8"},
		{Name: "timestamp", Type: "uint64"},
		{Name: "certs", Type: "bytes32[]"},
		{Name: "userData", Type: "bytes"},
		{Name: "nonce", Type: "bytes"},
		{Name: "publicKey", Type: "bytes"},
		{Name: "pcrs", Type: "tuple[]", Components: []abi.ArgumentMarshaling{
			{Name: "index", Type: "uint64"},
			{Name: "value", Type: "tuple", Components: []abi.ArgumentMarshaling{
				{Name: "first", Type: "bytes32"},
				{Name: "second", Type: "bytes16"},
			}},
		}},
		{Name: "moduleId", Type: "string"},
	})
	if err != nil {
		return nil, err
	}
	args := abi.Arguments{{Type: journalType}}
	return args.Pack(espressogen.VerifierJournal{
		Certs:     [][32]byte{},
		UserData:  []byte{},
		Nonce:     []byte{},
		PublicKey: publicKey,
		Pcrs:      []espressogen.Pcr{},
	})
}
