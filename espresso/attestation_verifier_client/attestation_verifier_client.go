package attestationverifierclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

type EspressoAttestationVerifierClient struct {
	baseURL string
	client  *http.Client
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
	if attestationServiceURL == "" {
		return nil
	}
	return &EspressoAttestationVerifierClient{
		baseURL: attestationServiceURL,
		client:  http.DefaultClient,
	}
}

func (c *EspressoAttestationVerifierClient) GenerateZKProof(ctx context.Context, attestationBytes []byte) (*OnchainProof, error) {
	request, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/generate_proof", bytes.NewBuffer(attestationBytes))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	res, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	responseData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var zkProof OnchainProof
	err = json.Unmarshal(responseData, &zkProof)
	if err != nil {
		return nil, err
	}

	return &zkProof, nil
}
