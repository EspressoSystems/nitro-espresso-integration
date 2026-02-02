package espressotee

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/offchainlabs/nitro/espresso-tee-contracts/espressogen"
)

type EspressoNitroTEEVerifierInterface interface {
	IsPCR0HashRegistered(pcr0Hash [32]byte, serviceType ServiceType) (bool, error)
}

type EspressoNitroTEEVerifier struct {
	l1Client *ethclient.Client
	address  common.Address
}

func NewEspressoNitroTEEVerifier(l1Client *ethclient.Client, nitroAddr common.Address) *EspressoNitroTEEVerifier {
	return &EspressoNitroTEEVerifier{l1Client: l1Client, address: nitroAddr}
}

func (e *EspressoNitroTEEVerifier) IsPCR0HashRegistered(pcr0Hash [32]byte, serviceType ServiceType) (bool, error) {
	switch serviceType {
	case BatchPoster:
	case CaffNode:
		return e.isPCR0HashRegistered(pcr0Hash, serviceType)
	}
	return false, fmt.Errorf("Invalid service type for checking PCR0 hash registration")

}

func (e *EspressoNitroTEEVerifier) isPCR0HashRegistered(pcr0Hash [32]byte, serviceType ServiceType) (bool, error) {
	contract, err := espressogen.NewEspressoNitroTEEVerifier(e.address, e.l1Client)
	if err != nil {
		return false, err
	}
	return contract.RegisteredEnclaveHash(&bind.CallOpts{}, pcr0Hash, uint8(serviceType))
}
