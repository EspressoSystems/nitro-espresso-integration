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

type ProofType string

const (
	ProofTypeVerifier   ProofType = "Verifier"
	ProofTypeAggregator ProofType = "Aggregator"
)

type ZkCoProcessorType string

const (
	ZkCoProcessorTypeSP1      ZkCoProcessorType = "SP1"
	ZkCoProcessorTypeRISCZero ZkCoProcessorType = "RISCZero"
)

type OnchainProof struct {
	ZkType       ZkCoProcessorType `json:"zktype"`
	ZkVMVersion  string            `json:"zkvm_version"`
	ProgramID    ProgramId         `json:"program_id"`
	RawProof     RawProof          `json:"raw_proof"`
	OnchainProof []byte            `json:"onchain_proof"`
	ProofType    ProofType         `json:"proof_type"`
}

type ProgramId struct {
	VerifierID      [32]byte `json:"verifier_id"`
	VerifierProofID [32]byte `json:"verifier_proof_id"`
	AggregatorID    [32]byte `json:"aggregator_id"`
}

type RawProof struct {
	EncodedProof []byte `json:"encoded_proof"`
	Journal      []byte `json:"journal"`
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
