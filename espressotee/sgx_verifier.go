package espressotee

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/offchainlabs/nitro/espressotee/legacy"
)

type EspressoSGXVerifierInterface interface {
	Verify(opts *bind.CallOpts, rawQuote []byte, reportDataHash [32]byte) (legacy.EnclaveReport, error)
}

type EspressoSGXVerifier struct {
	verifier *legacy.IEspressoSGXTEEVerifier
}

func (v *EspressoSGXVerifier) Verify(opts *bind.CallOpts, rawQuote []byte, reportDataHash [32]byte) (legacy.EnclaveReport, error) {
	return v.verifier.Verify(opts, rawQuote, reportDataHash)
}

func NewEspressoSGXVerifier(l1Client *ethclient.Client, addr common.Address) (*EspressoSGXVerifier, error) {
	verifier, err := legacy.NewIEspressoSGXTEEVerifier(addr, l1Client)
	if err != nil {
		return nil, err
	}
	return &EspressoSGXVerifier{verifier: verifier}, nil
}

type EnclaveReport = legacy.EnclaveReport
