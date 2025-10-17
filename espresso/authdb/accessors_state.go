package authdb

import (
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

// only check codeHash = Keccak(code), don't check if codeHash is part of the world state trie
func (d *AuthDB) authReadCodeWithPrefix(key []byte) ([]byte, error) {
	codeHash := parseHash(key)
	code, err := d.db.Get(key)
	if err != nil {
		return nil, err
	}
	if crypto.Keccak256Hash(code) != codeHash {
		log.Error("codeHash mismatch", "key", key, "code", code)
		return nil, fmt.Errorf("codeHash mismatch, key: %v, code: %v", key, code)
	}

	return code, nil
}

// only check hash = Keccak(preimage)
func (d *AuthDB) authReadPreimage(key []byte) ([]byte, error) {
	hash := parseHash(key)
	preimage, err := d.db.Get(key)
	if err != nil {
		return nil, err
	}
	if crypto.Keccak256Hash(preimage) != hash {
		log.Error("codeHash mismatch", "key", key, "preimage", preimage)
		return nil, fmt.Errorf("codeHash mismatch, key: %v, preimage: %v", key, preimage)
	}

	return preimage, nil
}
