package integrityattestation

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

// TestDeriveHmacDeterministic verifies that deriveHmac always produces
// the same HMAC key for the same ECDSA private key.
func TestDeriveHmacDeterministic(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate test private key: %v", err)
	}

	hmac1 := deriveHmac(privateKey)
	testMessage := []byte("test message")
	hmac1.Write(testMessage)
	sum1 := hmac1.Sum(nil)

	hmac2 := deriveHmac(privateKey)
	hmac2.Write(testMessage)
	sum2 := hmac2.Sum(nil)

	// Verify HMAC outputs are identical for the same private key and message
	if !bytes.Equal(sum1, sum2) {
		t.Errorf("HMAC outputs differ for the same private key and message.\nFirst:  %x\nSecond: %x", sum1, sum2)
	}
}

// TestDeriveHmacDifferentKeys verifies that different private keys
// produce different HMAC keys.
func TestDeriveHmacDifferentKeys(t *testing.T) {
	// Generate two different ECDSA private keys
	privateKey1, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate first test private key: %v", err)
	}

	privateKey2, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate second test private key: %v", err)
	}

	hmac1 := deriveHmac(privateKey1)
	hmac2 := deriveHmac(privateKey2)

	testMessage := []byte("test message")
	hmac1.Write(testMessage)
	hmac2.Write(testMessage)

	sum1 := hmac1.Sum(nil)
	sum2 := hmac2.Sum(nil)

	// Verify HMAC outputs are different for different private keys
	if bytes.Equal(sum1, sum2) {
		t.Errorf("HMAC outputs are identical for different private keys, expected them to differ")
	}
}
