package arbnode

import (
	"crypto/ecdsa"
	"encoding/asn1"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
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

func TestEspressoKeyManager(t *testing.T) {
	privKey := "1234567890abcdef1234567890abcdef12345678000000000000000000000000"
	mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
	mockEspressoTEEVerifierClient.On("RegisterSigner", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	wallet := DefaultBatchPosterL1WalletConfig
	wallet.PrivateKey = privKey
	opts := &BatchPosterOpts{
		ParentChainID: big.NewInt(1),
		Config: func() *BatchPosterConfig {
			return &BatchPosterConfig{
				ParentChainWallet: wallet,
			}
		},
	}

	// Test initialization
	t.Run("NewEspressoKeyManager", func(t *testing.T) {
		km := NewEspressoKeyManager(mockEspressoTEEVerifierClient, opts)
		require.NotNil(t, km, "Key manager should not be nil")
		assert.NotEmpty(t, km.pubKey, "Public key should be set")
		assert.NotNil(t, km.privKey, "Private key should be set")
		assert.False(t, km.HasRegistered(), "Should not be registered initially")
	})

	// Test HasRegistered and Registry
	t.Run("Registry", func(t *testing.T) {
		km := NewEspressoKeyManager(mockEspressoTEEVerifierClient, opts)
		assert.False(t, km.HasRegistered(), "Should start unregistered")

		// Mock sign function
		called := false
		signFunc := func(data []byte) ([]byte, error) {
			called = true
			assert.Equal(t, km.pubKey, data, "Sign function should receive public key")
			return []byte("mock-signature"), nil
		}

		// First registration
		err := km.Register(signFunc)
		require.NoError(t, err, "Registry should succeed")
		assert.True(t, called, "Sign function should be called")
		assert.True(t, km.HasRegistered(), "Should be registered after call")

		// Second call (already registered)
		called = false
		err = km.Register(signFunc)
		require.NoError(t, err, "Registry should succeed when already registered")
		assert.False(t, called, "Sign function should not be called again")
	})

	// Test GetCurrentKey
	t.Run("GetCurrentKey", func(t *testing.T) {
		km := NewEspressoKeyManager(mockEspressoTEEVerifierClient, opts)
		pubKey := km.GetCurrentKey()
		assert.NotEmpty(t, pubKey, "Public key should not be empty")
		assert.Equal(t, km.pubKey, pubKey, "GetCurrentKey should match initialized pubKey")

		assert.Equal(t, 65, len(pubKey), "Public key should be 65 bytes")
	})

	// Test Sign
	t.Run("SignBatch", func(t *testing.T) {
		km := NewEspressoKeyManager(mockEspressoTEEVerifierClient, opts)
		message := []byte("test-message")
		signature, err := km.SignBatch(message)
		require.NoError(t, err, "Sign should succeed")
		assert.NotEmpty(t, signature, "Signature should not be empty")

		// Check signature length (DER typically 70-72 bytes for secp256k1)
		assert.GreaterOrEqual(t, len(signature), 70, "Signature should be at least 70 bytes (DER)")
		assert.LessOrEqual(t, len(signature), 72, "Signature should be at most 72 bytes (DER)")

		// Parse DER signature
		var sig ecdsaSignature
		rest, err := asn1.Unmarshal(signature, &sig)
		require.NoError(t, err, "Should parse DER signature")
		assert.Empty(t, rest, "Should consume entire signature")

		// Verify r and s are non-zero
		assert.NotZero(t, sig.R, "r should be non-zero")
		assert.NotZero(t, sig.S, "s should be non-zero")

		// Verify signature with public key
		hash := crypto.Keccak256Hash(message)
		pubKey := km.privKey.Public()
		ecdsaPubkey, ok := pubKey.(*ecdsa.PublicKey)
		require.True(t, ok, "Public key should be an ecdsa.PublicKey")
		valid := ecdsa.Verify(ecdsaPubkey, hash.Bytes(), sig.R, sig.S)
		assert.True(t, valid, "Signature should verify with public key")
	})
}
