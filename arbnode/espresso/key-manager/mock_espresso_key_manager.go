package keymanager

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	crypto_rand "crypto/rand"
	"fmt"

	"github.com/offchainlabs/nitro/espressotee"
)

// MockEspressoKeyManager is a mock implementation of the
// arbnode.EspressoKeyManagerInterface.
//
// It is used for testing purposes and provides a simple implementation
// of the Espresso key management functionality without requiring a real
// Espresso environment.
type MockEspressoKeyManager struct {
	Key *ecdsa.PrivateKey
}

var _ EspressoKeyManagerInterface = &MockEspressoKeyManager{}

// NewMockEspressoKeyManager creates a new instance of MockEspressoKeyManager
// with a randomly generated private key.
func NewMockEspressoKeyManager() *MockEspressoKeyManager {
	privKey, err := ecdsa.GenerateKey(elliptic.P224(), crypto_rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("failed to generate mock private key: %v", err))
	}

	return &MockEspressoKeyManager{
		Key: privKey,
	}
}

// GetCurrentKey implements arbnode.EspressoKeyManagerInterface.
func (m *MockEspressoKeyManager) GetCurrentKey() *ecdsa.PublicKey {
	return &m.Key.PublicKey
}

// HasRegistered implements arbnode.EspressoKeyManagerInterface.
func (m *MockEspressoKeyManager) HasRegistered() bool {
	return false
}

// Register implements arbnode.EspressoKeyManagerInterface.
func (m *MockEspressoKeyManager) Register(getAttestationFunc func([]byte) ([]byte, error)) error {
	return nil
}

// SignBatch implements arbnode.EspressoKeyManagerInterface.
func (m *MockEspressoKeyManager) SignBatch(message []byte) ([]byte, error) {
	return m.Key.Sign(crypto_rand.Reader, message, crypto.SHA256)
}

// SignHotShotPayload implements arbnode.EspressoKeyManagerInterface.
func (m *MockEspressoKeyManager) SignHotShotPayload(message []byte) ([]byte, error) {
	return m.Key.Sign(crypto_rand.Reader, message, crypto.SHA256)
}

// TeeType implements arbnode.EspressoKeyManagerInterface.
func (m *MockEspressoKeyManager) TeeType() espressotee.TEE {
	return espressotee.SGX
}
