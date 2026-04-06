package keymanager_test

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	hdwallet "github.com/miguelmota/go-ethereum-hdwallet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/offchainlabs/nitro/arbnode/dataposter"
	espresso_key_manager "github.com/offchainlabs/nitro/espresso/key-manager"
	"github.com/offchainlabs/nitro/espressotee"
)

type mockEspressoTEEVerifier struct {
	mock.Mock
}

func (m *mockEspressoTEEVerifier) RegisterService(dataPoster *dataposter.DataPoster, attestation []byte, data []byte, teeType uint8, serviceType espressotee.ServiceType) error {
	args := m.Called(dataPoster, attestation, data, teeType)
	return args.Error(0)
}

func (m *mockEspressoTEEVerifier) RegisteredServices(addr common.Address, teeType espressotee.TEE, serviceType espressotee.ServiceType) (bool, error) {
	args := m.Called(addr)
	return args.Bool(0), nil
}

func (m *mockEspressoTEEVerifier) CheckNonceValidation(dataPoster *dataposter.DataPoster) error {
	args := m.Called(dataPoster)
	return args.Error(0)
}

func TestEspressoKeyManager(t *testing.T) {
	privKey := "1234567890abcdef1234567890abcdef12345678000000000000000000000000"

	_, signer, err := GetTransactOptsAndSigner(privKey, big.NewInt(1))
	require.NoError(t, err, "Should open wallet")
	dataSigner := func(data []byte) ([]byte, error) { return signer(data) }
	dataposter := &dataposter.DataPoster{}

	// Generate persistent private key from test mnemonic for tests that need it
	testMnemonic := "test test test test test test test test test test test junk"
	persistentPrivKey := GeneratePrivateKeyFromMnemonic(t, testMnemonic, 0)

	// Test initialization
	t.Run("SGX NewEspressoKeyManager", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterService", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockEspressoTEEVerifierClient.On("RegisteredServices", mock.Anything).Return(false, nil).Once()
		km := espresso_key_manager.NewEspressoKeyManager(mockEspressoTEEVerifierClient, dataposter, dataSigner, espresso_key_manager.SGX, espressotee.Test, persistentPrivKey, "", "", 0)
		require.NotNil(t, km, "Key manager should not be nil")
		assert.NotEmpty(t, km.GetCurrentKey(), "Public key should be set")
		// assert.NotNil(t, km.privKey, "Private key should be set")
		state := km.GetKeyManagerState()
		assert.Equal(t, espresso_key_manager.Init, state, "Should not be registered initially")
	})

	// Test HasRegistered and Registry
	t.Run("SGX Registry", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterService", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		// We must return false then true to simulate a valid registration
		mockEspressoTEEVerifierClient.On("RegisteredServices", mock.Anything).Return(false, nil).Once()
		mockEspressoTEEVerifierClient.On("RegisteredServices", mock.Anything).Return(true, nil).Once()
		mockEspressoTEEVerifierClient.On("CheckNonceValidation", mock.Anything).Return(nil)
		km := espresso_key_manager.NewEspressoKeyManager(mockEspressoTEEVerifierClient, dataposter, dataSigner, espresso_key_manager.TESTS, espressotee.Test, persistentPrivKey, "", "", 0)

		assert.Equal(t, espresso_key_manager.Init, km.GetKeyManagerState(), "Should start unregistered")

		// Init → PendingDataPosterSync
		registered, err := km.CheckRegistration()
		require.NoError(t, err)
		assert.False(t, registered)
		assert.Equal(t, espresso_key_manager.PendingDataPosterSync, km.GetKeyManagerState())

		// PendingDataPosterSync → PendingRegistration
		registered, err = km.CheckRegistration()
		require.NoError(t, err)
		assert.False(t, registered)
		assert.Equal(t, espresso_key_manager.PendingRegistration, km.GetKeyManagerState())

		// PendingRegistration → Registered
		registered, err = km.CheckRegistration()
		require.NoError(t, err)
		assert.True(t, registered)
		assert.Equal(t, espresso_key_manager.Registered, km.GetKeyManagerState())
	})

	// Test GetCurrentKey
	t.Run("GetCurrentKey", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterService", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockEspressoTEEVerifierClient.On("RegisteredServices", mock.Anything).Return(false, nil).Once()
		km := espresso_key_manager.NewEspressoKeyManager(mockEspressoTEEVerifierClient, dataposter, dataSigner, espresso_key_manager.SGX, espressotee.Test, persistentPrivKey, "", "", 0)
		pubKey := km.GetCurrentKey()
		assert.NotEmpty(t, pubKey, "Public key should not be empty")
	})

	// Test Sign
	t.Run("SGX SignMessage with the ephemeral key", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterService", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockEspressoTEEVerifierClient.On("RegisteredServices", mock.Anything).Return(false, nil).Once()
		km := espresso_key_manager.NewEspressoKeyManager(mockEspressoTEEVerifierClient, dataposter, dataSigner, espresso_key_manager.SGX, espressotee.Test, persistentPrivKey, "", "", 0)
		message := []byte("test-message")
		signature, err := km.SignMessage(message)
		require.NoError(t, err, "Sign should succeed")
		assert.NotEmpty(t, signature, "Signature should not be empty")

		ecdsaPubkey := km.GetCurrentKey()
		valid, err := VerifySignatureWithPublicKey(ecdsaPubkey, message, signature)
		require.NoError(t, err, "Should verify signature")
		assert.True(t, valid, "Signature should verify with public key")
	})

	t.Run("SGX Sign Hotshot payload with batcher private key", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterService", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockEspressoTEEVerifierClient.On("RegisteredServices", mock.Anything).Return(false, nil).Once()
		km := espresso_key_manager.NewEspressoKeyManager(mockEspressoTEEVerifierClient, dataposter, dataSigner, espresso_key_manager.SGX, espressotee.Test, persistentPrivKey, "", "", 0)
		message := []byte("test-message")
		signature, err := km.SignPayload(message)
		require.NoError(t, err, "Sign should succeed")

		privKeyBytes, err := hex.DecodeString(privKey)
		assert.NoError(t, err, "Should decode private key")
		pk, err := crypto.ToECDSA(privKeyBytes)
		assert.NoError(t, err, "Should convert private key to ECDSA")

		ecdsaPubkey, ok := pk.Public().(*ecdsa.PublicKey)
		require.True(t, ok, "Public key should be an ecdsa.PublicKey")
		valid, err := VerifySignatureWithPublicKey(ecdsaPubkey, message, signature)
		require.NoError(t, err, "Should verify signature")
		assert.True(t, valid, "Signature should verify with public key")
	})

	t.Run("Nitro Registry", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterService", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockEspressoTEEVerifierClient.On("RegisteredServices", mock.Anything).Return(false, nil).Once()
		mockEspressoTEEVerifierClient.On("RegisteredServices", mock.Anything).Return(true, nil).Once()
		mockEspressoTEEVerifierClient.On("CheckNonceValidation", mock.Anything).Return(nil)
		km := espresso_key_manager.NewEspressoKeyManager(mockEspressoTEEVerifierClient, dataposter, dataSigner, espresso_key_manager.TESTS, espressotee.Test, nil, "", "", 0)
		assert.Equal(t, espresso_key_manager.Init, km.GetKeyManagerState(), "Should start unregistered")

		// Init → PendingDataPosterSync
		registered, err := km.CheckRegistration()
		require.NoError(t, err)
		assert.False(t, registered)
		assert.Equal(t, espresso_key_manager.PendingDataPosterSync, km.GetKeyManagerState())

		// PendingDataPosterSync → PendingRegistration
		registered, err = km.CheckRegistration()
		require.NoError(t, err)
		assert.False(t, registered)
		assert.Equal(t, espresso_key_manager.PendingRegistration, km.GetKeyManagerState())

		// PendingRegistration → Registered
		registered, err = km.CheckRegistration()
		require.NoError(t, err)
		assert.True(t, registered)
		assert.Equal(t, espresso_key_manager.Registered, km.GetKeyManagerState())
	})

	// Test Sign
	t.Run("Nitro SignMessage with the ephemeral key", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterService", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockEspressoTEEVerifierClient.On("RegisteredServices", mock.Anything).Return(false, nil).Once()
		km := espresso_key_manager.NewEspressoKeyManager(mockEspressoTEEVerifierClient, dataposter, dataSigner, espresso_key_manager.NITRO, espressotee.Test, persistentPrivKey, "", "", 0)
		message := []byte("test-message")
		signature, err := km.SignMessage(message)
		require.NoError(t, err, "Sign should succeed")
		assert.NotEmpty(t, signature, "Signature should not be empty")

		ecdsaPubkey := km.GetCurrentKey()
		valid, err := VerifySignatureWithPublicKey(ecdsaPubkey, message, signature)
		require.NoError(t, err, "Should verify signature")
		assert.True(t, valid, "Signature should verify with public key")
	})

	t.Run("Nitro Sign Hotshot payload with batcher private key", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterService", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockEspressoTEEVerifierClient.On("RegisteredServices", mock.Anything).Return(false, nil).Once()
		km := espresso_key_manager.NewEspressoKeyManager(mockEspressoTEEVerifierClient, dataposter, dataSigner, espresso_key_manager.NITRO, espressotee.Test, persistentPrivKey, "", "", 0)
		message := []byte("test-message")
		signature, err := km.SignPayload(message)
		require.NoError(t, err, "Sign should succeed")

		privKeyBytes, err := hex.DecodeString(privKey)
		assert.NoError(t, err, "Should decode private key")
		pk, err := crypto.ToECDSA(privKeyBytes)
		assert.NoError(t, err, "Should convert private key to ECDSA")

		ecdsaPubkey, ok := pk.Public().(*ecdsa.PublicKey)
		require.True(t, ok, "Public key should be an ecdsa.PublicKey")
		valid, err := VerifySignatureWithPublicKey(ecdsaPubkey, message, signature)
		require.NoError(t, err, "Should verify signature")
		assert.True(t, valid, "Signature should verify with public key")
	})
}

func VerifySignatureWithPublicKey(publicKey *ecdsa.PublicKey, data []byte, signature []byte) (bool, error) {
	hash := crypto.Keccak256Hash(data)
	recoveredPubKey, err := crypto.SigToPub(hash.Bytes(), signature)
	if err != nil {
		return false, err
	}
	return crypto.PubkeyToAddress(*recoveredPubKey) == crypto.PubkeyToAddress(*publicKey), nil
}

func GetTransactOptsAndSigner(priKey string, chainId *big.Int) (*bind.TransactOpts, DataSignerFunc, error) {
	privateKey, err := crypto.HexToECDSA(priKey)
	if err != nil {
		return nil, nil, err
	}
	var txOpts *bind.TransactOpts
	if chainId != nil {
		txOpts, err = bind.NewKeyedTransactorWithChainID(privateKey, chainId)
		if err != nil {
			return nil, nil, err
		}
	}
	signer := func(data []byte) ([]byte, error) {
		return crypto.Sign(data, privateKey)
	}

	return txOpts, signer, nil
}

type DataSignerFunc func([]byte) ([]byte, error)

// GeneratePrivateKeyFromMnemonic generates a private key from a mnemonic phrase
func GeneratePrivateKeyFromMnemonic(t *testing.T, mnemonic string, index uint) *ecdsa.PrivateKey {
	wallet, err := hdwallet.NewFromMnemonic(mnemonic)
	require.NoError(t, err, "Should create wallet from mnemonic")

	path := hdwallet.MustParseDerivationPath(fmt.Sprintf("m/44'/60'/0'/0/%d", index))
	account, err := wallet.Derive(path, false)
	require.NoError(t, err, "Should derive account from path")

	privateKey, err := wallet.PrivateKey(account)
	require.NoError(t, err, "Should get private key from wallet")

	return privateKey
}
