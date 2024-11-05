package espressocrypto

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"testing"
)

type MerkleProofTestData struct {
	Proof             json.RawMessage `json:"proof"`
	Header            json.RawMessage `json:"header"`
	HeaderCommitment  []uint8         `json:"header_commitment"`
	BlockMerkleRoot   string          `json:"block_merkle_root"`
	HotShotCommitment []uint8         `json:"hotshot_commitment"`
}

func TestMerkleProofVerification(t *testing.T) {
	file, err := os.Open("./merkle_proof_test_data.json")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read file")
	}

	var data MerkleProofTestData

	if err := json.Unmarshal(bytes, &data); err != nil {
		log.Fatalf("")
	}

	r := verifyMerkleProof(data.Proof, data.Header, []byte(data.BlockMerkleRoot), data.HotShotCommitment)
	if !r {
		log.Fatalf("Error")
	}

	// Tamper with the correct data and see if it will return false
	data.HotShotCommitment[0] = 1

	r = verifyMerkleProof(data.Proof, data.Header, []byte(data.BlockMerkleRoot), data.HotShotCommitment)
	if r {
		log.Fatalf("Error")
	}

}
