package espressotee

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	legacy_espressogen "github.com/offchainlabs/nitro/espresso-tee-contracts-legacy/espressogen"
	"github.com/offchainlabs/nitro/espresso-tee-contracts/espressogen"
)

type EspressoNitroTEEVerifierInterface interface {
	IsPCR0HashRegistered(pcr0Hash [32]byte, serviceType ServiceType) (bool, error)
}

type EspressoNitroTEEVerifier struct {
	contract *espressogen.IEspressoNitroTEEVerifier
	l1Client *ethclient.Client
	address  common.Address
}

func NewEspressoNitroTEEVerifier(contract *espressogen.IEspressoNitroTEEVerifier, l1Client *ethclient.Client, nitroAddr common.Address) *EspressoNitroTEEVerifier {
	return &EspressoNitroTEEVerifier{contract: contract, l1Client: l1Client, address: nitroAddr}
}

func (e *EspressoNitroTEEVerifier) IsPCR0HashRegistered(pcr0Hash [32]byte) (bool, error) {
	return e.contract.RegisteredEnclaveHash(&bind.CallOpts{}, pcr0Hash)
}

/**
 * This functions checks and verifies a certificate on-chain.
 * Always verify certificate on chain, if certificate is already verified it is very cheap to verify again on chain
 */
func (e *EspressoNitroTEEVerifier) VerifyCert(
	dataPoster *dataposter.DataPoster,
	certificate []byte, parentCertHash [32]byte,
	isCA bool,
	registerSignerOpts EspressoRegisterSignerOpts,
) (common.Hash, error) {
	// Get certificate hash
	certHash := crypto.Keccak256Hash(certificate)

	// Try and verify the certificate either CA or client
	contractABI, err := espressogen.IEspressoNitroTEEVerifierMetaData.GetAbi()
	if err != nil {
		return certHash, err
	}

