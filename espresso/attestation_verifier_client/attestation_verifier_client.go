package attestationverifierclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type EspressoAttestationVerifierClient struct {
	baseURL string
}

type OnchainProof struct {
	Zktype      string `json:"zktype"`
	ZkvmVersion string `json:"zkvm_version"`
	ProgramID   struct {
		VerifierID      string `json:"verifier_id"`
		VerifierProofID string `json:"verifier_proof_id"`
		AggregatorID    string `json:"aggregator_id"`
	} `json:"program_id"`
	RawProof struct {
		EncodedProof string `json:"encoded_proof"`
		Journal      string `json:"journal"`
	} `json:"raw_proof"`
	OnchainProof string `json:"onchain_proof"`
	ProofType    string `json:"proof_type"`
}

func NewEspressoAttestationVerifierClient(
	attestationServiceURL string,
) *EspressoAttestationVerifierClient {
	return &EspressoAttestationVerifierClient{
		baseURL: strings.TrimSuffix(attestationServiceURL, "/"),
	}
}

func (c *EspressoAttestationVerifierClient) GenerateZKProof(ctx context.Context, attestationBytes []byte) (*OnchainProof, error) {
	request, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/generate_proof", bytes.NewBuffer(attestationBytes))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/octet-stream")

	client := http.Client{
		Timeout: 2 * time.Minute,
	}
	res, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	responseData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("attestation service returned status %d: %s", res.StatusCode, string(responseData))
	}

	var zkProof OnchainProof
	err = json.Unmarshal(responseData, &zkProof)
	if err != nil {
		return nil, err
	}

	return &zkProof, nil
}
