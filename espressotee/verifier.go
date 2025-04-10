package espressotee

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/offchainlabs/nitro/solgen/go/espressogen"
)

type EspressoTEEVerifierInterface interface {
	Verify(opts *bind.CallOpts, signature []byte, userDataHash [32]byte, teeType uint8) (bool, error)
}

type EspressoTEEVerifier struct {
	teeType  uint8
	l1Client *ethclient.Client
	verifier *espressogen.IEspressoTEEVerifier
}

func NewEspressoTEEVerifier(l1Client *ethclient.Client, addr common.Address, teeType uint8) (*EspressoTEEVerifier, error) {
	verifier, err := espressogen.NewIEspressoTEEVerifier(addr, l1Client)
	if err != nil {
		return nil, err
	}
	return &EspressoTEEVerifier{
		teeType:  teeType,
		l1Client: l1Client,
		verifier: verifier,
	}, nil
}

func (e *EspressoTEEVerifier) Verify(opts *bind.CallOpts, signature []byte, userDataHash [32]byte, teeType uint8) (bool, error) {
	return e.verifier.Verify(opts, signature, userDataHash, e.teeType)
}
