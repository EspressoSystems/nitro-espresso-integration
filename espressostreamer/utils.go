package espressostreamer

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcutil/base58"
	"github.com/zeebo/blake3"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

const (
	FilterAndFind_Remove = iota
	FilterAndFind_Keep
	FilterAndFind_Target
)

// FilterAndFind filters an array in-place and returns the matching element based on a comparison function.
// The comparison function should return:
//   - FilterAndFindTarget for the element to be returned, will be kept in the array
//   - FilterAndFindKeep for elements to be kept
//   - FilterAndFindRemove for elements to be removed
//
// Returns the index of the found element (if any)
func FilterAndFind[T any](arr *[]T, compareFunc func(T) int) int {

	var hasFound bool
	idx := -1

	if arr == nil || len(*arr) == 0 {
		return idx
	}

	// `j` is the next legal index to insert an element
	j := 0
	for i := 0; i < len(*arr); i++ {
		result := compareFunc((*arr)[i])

		if result == FilterAndFind_Remove || (result == FilterAndFind_Target && hasFound) {
			// here we skip the element and do not increment `j`
			continue
		}

		// Take the first element that matches
		if result == FilterAndFind_Target {
			hasFound = true
			idx = j
		}
		if i != j {
			// current element should be kept, so we move it to the next legal index `j`.
			(*arr)[j] = (*arr)[i]
		}
		j++
	}

	// now `j` is the length of elements to keep, we truncate the array to the new length
	*arr = (*arr)[:j]
	return idx
}

// CountUniqueEntries iterates over an array with potential duplicate values and counts the unique entries.
// returns a Uint that represents the number of unique entries.
// @Dev:
func CountUniqueEntries[T any](arr *[]T) uint64 {
	var uniqueCount uint64 // Declare the variable before assignment so the compiler doesn't infer it as an int.
	entriesMap := make(map[any]bool)
	uniqueCount = 0
	for _, entry := range *arr {
		if !entriesMap[entry] {
			uniqueCount += 1
			entriesMap[entry] = true
		}

	}
	return uniqueCount
}

// Get the timeboost calculated block hash
// See: https://github.com/EspressoSystems/timeboost/blob/ad534f3d7c6485e80b265811073d4e242dfd0746/timeboost-types/src/block.rs#L150-L155
func GetTimeboostBlockHash(round uint64, payload []byte) ([]byte, error) {
	hasher := blake3.New()
	roundBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(roundBytes, round)
	if _, err := hasher.Write(roundBytes); err != nil {
		return nil, fmt.Errorf("failed to write round to hasher: %w", err)
	}
	if _, err := hasher.Write(payload); err != nil {
		return nil, fmt.Errorf("failed to write payload to hasher: %w", err)
	}
	return hasher.Sum(nil), nil
}

// Validate the signatures in the timeboost generate certificate against the committee for one honest threshold
func ValidateTimeboostCertificate(commitment []byte, sigs map[uint8][]byte) error {
	// TODO: These should be read from contract, for now use the hard coded keyset.json
	publicKeyMap := map[uint8][]byte{
		0: base58.Decode("qkoZ7xPFuTjNpKmn3SyWL2Y6WLm89wi9jNkDuu9KefXv"),
		1: base58.Decode("28y18s4egBUnxoLSJY8vCYXV8KXaKYysD6tUen7syFyPt"),
	}

	validSigs := 0
	for keyID, sig := range sigs {
		btcecPubKey, err := btcec.ParsePubKey(publicKeyMap[keyID])
		if err != nil {
			return err
		}
		compressedBytes := btcecPubKey.SerializeCompressed()
		hasher := sha256.New()
		if _, err := hasher.Write(commitment); err != nil {
			return err
		}
		if !crypto.VerifySignature(compressedBytes, hasher.Sum(nil), sig) {
			log.Error("signature verification failed for key ID", "id", keyID)
			continue
		}
		validSigs += 1
	}
	oneHonestThreshold := (len(publicKeyMap)-1)/3 + 1
	if validSigs < oneHonestThreshold {
		return fmt.Errorf("not enough signatures found in certificate. wanted: %d have: %d", oneHonestThreshold, validSigs)
	}
	return nil
}
