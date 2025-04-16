package espressotee

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/offchainlabs/nitro/solgen/go/espressogen"
)

type EspressoTEEVerifierInterface interface {
	Verify(opts *bind.CallOpts, signature []byte, userDataHash [32]byte) (bool, error)
	VerifyLegacy(opts *bind.CallOpts, signature []byte, userDataHash [32]byte) (bool, error)
	GetLegacySgxVerifier() *espressogen.IEspressoSGXTEEVerifier
}

type EspressoTEEVerifier struct {
	teeType           uint8
	l1Client          *ethclient.Client
	verifier          *espressogen.IEspressoTEEVerifier
	legacySgxVerifier *espressogen.IEspressoSGXTEEVerifier
}

func NewEspressoTEEVerifier(
	l1Client *ethclient.Client,
	addr common.Address,
	teeType uint8,
	legacyAddr *common.Address,
) (*EspressoTEEVerifier, error) {
	verifier, err := espressogen.NewIEspressoTEEVerifier(addr, l1Client)
	if err != nil {
		return nil, err
	}

	var legacySgxVerifier *espressogen.IEspressoSGXTEEVerifier
	if legacyAddr != nil {
		legacySgxVerifier, err = espressogen.NewIEspressoSGXTEEVerifier(*legacyAddr, l1Client)
		if err != nil {
			return nil, err
		}
	}
	return &EspressoTEEVerifier{
		teeType:           teeType,
		l1Client:          l1Client,
		verifier:          verifier,
		legacySgxVerifier: legacySgxVerifier,
	}, nil
}

func (e *EspressoTEEVerifier) Verify(opts *bind.CallOpts, signature []byte, userDataHash [32]byte) (bool, error) {
	return e.verifier.Verify(opts, signature, userDataHash, e.teeType)
}

func (e *EspressoTEEVerifier) VerifyLegacy(opts *bind.CallOpts, signature []byte, userDataHash [32]byte) (bool, error) {
	if e.legacySgxVerifier == nil {
		return false, nil
	}
	_, err := e.legacySgxVerifier.Verify(opts, signature, userDataHash)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (e *EspressoTEEVerifier) GetLegacySgxVerifier() *espressogen.IEspressoSGXTEEVerifier {
	return e.legacySgxVerifier
}
