package espressotee

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	legacy_espressogen "github.com/offchainlabs/nitro/espresso-tee-contracts-legacy/espressogen"
)

type EspressoSGXVerifierInterface interface {
	Verify(opts *bind.CallOpts, rawQuote []byte, reportDataHash [32]byte) (legacy_espressogen.EnclaveReport, error)
}

type EspressoSGXVerifier struct {
	verifier *legacy_espressogen.IEspressoSGXTEEVerifier
}

func (v *EspressoSGXVerifier) Verify(opts *bind.CallOpts, rawQuote []byte, reportDataHash [32]byte) (legacy_espressogen.EnclaveReport, error) {
	return v.verifier.Verify(opts, rawQuote, reportDataHash)
}

func NewEspressoSGXVerifier(l1Client *ethclient.Client, addr common.Address) (*EspressoSGXVerifier, error) {
	verifier, err := legacy_espressogen.NewIEspressoSGXTEEVerifier(addr, l1Client)
	if err != nil {
		return nil, err
	}
	return &EspressoSGXVerifier{verifier: verifier}, nil
}
