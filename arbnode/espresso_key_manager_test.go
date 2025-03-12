package arbnode

import (
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type ecdsaSignature struct {
	R, S *big.Int
}

type mockEspressoTEEVerifier struct {
	mock.Mock
}

func (m *mockEspressoTEEVerifier) RegisterSigner(opts *bind.TransactOpts, attestation []byte, pubKey []byte, teeType uint8) error {
	args := m.Called(opts, attestation, pubKey, teeType)
	return args.Error(0)
}

func (m *mockEspressoTEEVerifier) RegisteredSigners(addr common.Address, teeType uint8) (bool, error) {
	args := m.Called(addr, teeType)
	return args.Bool(0), nil
}

func TestEspressoKeyManager(t *testing.T) {
	privKey := "1234567890abcdef1234567890abcdef12345678000000000000000000000000"

	tranOpts, signer, err := GetTransactOptsAndSigner(privKey, big.NewInt(1))
	require.NoError(t, err, "Should open wallet")
	opts := &BatchPosterOpts{
		TransactOpts: tranOpts,
		DataSigner:   func(data []byte) ([]byte, error) { return signer(data) },
	}

	// Test initialization
	t.Run("NewEspressoKeyManager", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterSigner", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockEspressoTEEVerifierClient.On("RegisteredSigners", mock.Anything, mock.Anything).Return(false, nil).Once()
		km := NewEspressoKeyManager(mockEspressoTEEVerifierClient, opts)
		require.NotNil(t, km, "Key manager should not be nil")
		assert.NotEmpty(t, km.pubKey, "Public key should be set")
		assert.NotNil(t, km.privKey, "Private key should be set")
		registered, _ := km.HasRegistered()
		assert.False(t, registered, "Should not be registered initially")
	})

	// Test HasRegistered and Registry
	t.Run("Registry", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterSigner", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockEspressoTEEVerifierClient.On("RegisteredSigners", mock.Anything, mock.Anything).Return(false, nil).Once()
		mockEspressoTEEVerifierClient.On("RegisteredSigners", mock.Anything, mock.Anything).Return(true, nil).Maybe()
		km := NewEspressoKeyManager(mockEspressoTEEVerifierClient, opts)
		registered, _ := km.HasRegistered()
		assert.False(t, registered, "Should start unregistered")

		// Mock sign function
		called := false
		signFunc := func(data []byte) ([]byte, error) {
			called = true
			addr := crypto.PubkeyToAddress(*km.pubKey)
			addrBytes := addr.Bytes()
			assert.Equal(t, addrBytes, data, "Sign function should receive public key")
			return []byte("mock-signature"), nil
		}

		// First registration
		err := km.Register(signFunc)
		require.NoError(t, err, "Registry should succeed")
		assert.True(t, called, "Sign function should be called")
		registered, _ = km.HasRegistered()
		assert.True(t, registered, "Should be registered after call")

		// Second call (already registered)
		called = false
		err = km.Register(signFunc)
		require.NoError(t, err, "Registry should succeed when already registered")
		assert.False(t, called, "Sign function should not be called again")
	})

	// Test GetCurrentKey
	t.Run("GetCurrentKey", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterSigner", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockEspressoTEEVerifierClient.On("RegisteredSigners", mock.Anything, mock.Anything).Return(false, nil).Once()
		km := NewEspressoKeyManager(mockEspressoTEEVerifierClient, opts)
		pubKey := km.GetCurrentKey()
		assert.NotEmpty(t, pubKey, "Public key should not be empty")
		assert.Equal(t, km.pubKey, pubKey, "GetCurrentKey should match initialized pubKey")
	})

	// Test Sign
	t.Run("SignBatch with the ephemeral key", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterSigner", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockEspressoTEEVerifierClient.On("RegisteredSigners", mock.Anything, mock.Anything).Return(false, nil).Once()
		km := NewEspressoKeyManager(mockEspressoTEEVerifierClient, opts)
		message := []byte("test-message")
		signature, err := km.SignBatch(message)
		require.NoError(t, err, "Sign should succeed")
		assert.NotEmpty(t, signature, "Signature should not be empty")

		ecdsaPubkey, ok := km.privKey.Public().(*ecdsa.PublicKey)
		require.True(t, ok, "Public key should be an ecdsa.PublicKey")
		valid, err := VerifySignatureWithPublicKey(ecdsaPubkey, message, signature)
		require.NoError(t, err, "Should verify signature")
		assert.True(t, valid, "Signature should verify with public key")
	})

	t.Run("Sign Hotshot payload with batcher private key", func(t *testing.T) {
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
		mockEspressoTEEVerifierClient.On("RegisterSigner", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockEspressoTEEVerifierClient.On("RegisteredSigners", mock.Anything, mock.Anything).Return(false, nil).Once()
		km := NewEspressoKeyManager(mockEspressoTEEVerifierClient, opts)
		message := []byte("test-message")
		signature, err := km.SignHotShotPayload(message)
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

	matches := recoveredPubKey.Equal(publicKey)
	return matches, nil
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
