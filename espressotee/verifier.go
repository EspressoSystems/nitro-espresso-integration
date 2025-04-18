package espressotee

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/offchainlabs/nitro/solgen/go/espressogen"
)

type LegacySGXVerifierInterface interface {
	Verify(opts *bind.CallOpts, attestation []byte, signature [32]byte) (espressogen.EnclaveReport, error)
}

type LegacySGXVerifier struct {
	verifier *espressogen.IEspressoSGXTEEVerifier
}

func (v *LegacySGXVerifier) Verify(opts *bind.CallOpts, attestation []byte, signature [32]byte) (espressogen.EnclaveReport, error) {
	return v.verifier.Verify(opts, attestation, signature)
}

func NewLegacySGXVerifier(l1Client *ethclient.Client, addr common.Address) (*LegacySGXVerifier, error) {
	verifier, err := espressogen.NewIEspressoSGXTEEVerifier(addr, l1Client)
	if err != nil {
		return nil, err
	}
	return &LegacySGXVerifier{verifier: verifier}, nil
}
