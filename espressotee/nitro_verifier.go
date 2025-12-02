package espressotee

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbnode/dataposter"
	legacy_espressogen "github.com/offchainlabs/nitro/espresso-tee-contracts-legacy/espressogen"
	"github.com/offchainlabs/nitro/espresso-tee-contracts/espressogen"
)

type EspressoNitroTEEVerifierInterface interface {
	VerifyCert(
		dataPoster *dataposter.DataPoster,
		certificate []byte,
		parentCertHash [32]byte,
		isCA bool,
		registerSignerOpts EspressoRegisterServiceOpts,
		serviceType ServiceType,
	) (common.Hash, error)
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
		return e.isPCR0HashRegisteredLegacy(pcr0Hash)
	case CaffNode:
		return e.isPCR0HashRegistered(pcr0Hash)
	default:
		return false, fmt.Errorf("Invalid service type for checking PCR0 hash registration")
	}
}

func (e *EspressoNitroTEEVerifier) isPCR0HashRegisteredLegacy(pcr0Hash [32]byte) (bool, error) {
	contract, err := legacy_espressogen.NewEspressoNitroTEEVerifier(e.address, e.l1Client)
	if err != nil {
		return false, err
	}
	return contract.RegisteredEnclaveHash(&bind.CallOpts{}, pcr0Hash)
}

func (e *EspressoNitroTEEVerifier) isPCR0HashRegistered(pcr0Hash [32]byte) (bool, error) {
	contract, err := espressogen.NewEspressoNitroTEEVerifier(e.address, e.l1Client)
	if err != nil {
		return false, err
	}
	return contract.RegisteredCaffNodeEnclaveHashes(&bind.CallOpts{}, pcr0Hash)
}

/**
 * This functions checks and verifies a certificate on-chain.
 * Always verify certificate on chain, if certificate is already verified it is very cheap to verify again on chain
 */
func (e *EspressoNitroTEEVerifier) VerifyCert(
	dataPoster *dataposter.DataPoster,
	certificate []byte, parentCertHash [32]byte,
	isCA bool,
	registerSignerOpts EspressoRegisterServiceOpts,
	serviceType ServiceType,
) (common.Hash, error) {
	// Get certificate hash
	certHash := crypto.Keccak256Hash(certificate)

	// Try and verify the certificate either CA or client
	contractABI, err := espressogen.IEspressoNitroTEEVerifierMetaData.GetAbi()
	if err != nil {
		return certHash, err
	}

	// Always reverify the certificate, this is cheap once verified
	// Pack the function arguments (cerificate, parentCertHash)
	var calldata []byte
	if isCA {
		calldata, err = contractABI.Pack("verifyCACert", certificate, parentCertHash)
	} else {
		calldata, err = contractABI.Pack("verifyClientCert", certificate, parentCertHash)
	}
	if err != nil {
		return certHash, err
	}
	msg := ethereum.CallMsg{
		From:  dataPoster.Sender(),
		To:    &e.address,
		Data:  calldata,
		Value: dataPoster.Auth().Value,
	}
	estimate, err := e.l1Client.EstimateGas(context.Background(), msg)
	if err != nil {
		return certHash, err
	}
	err = NonceValidation(context.Background(), e.l1Client, dataPoster)
	if err != nil {
		return certHash, err
	}
	// Add a buffer to the estimate for the gas limit
	gasLimit := estimate * (100 + registerSignerOpts.GasLimitBufferIncreasePercent) / 100
	log.Info("verify cert gas limit", "gas limit", gasLimit)

	// Since we use batch poster private key to register signer, we need to use dataposter to post transaction
	// So the dataposter can track the proper nonce once we start posting batches
	tx, err := dataPoster.PostSimpleTransaction(
		context.Background(),
		e.address,
		calldata,
		gasLimit,
		dataPoster.Auth().Value,
	)
	if err != nil {
		return certHash, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), registerSignerOpts.MaxTxnWaitTime)
	defer cancel()

	log.Info("Waiting for cert tx to be mined",
		"tx", tx.Hash().Hex(),
		"isCA", isCA,
		"timeout", registerSignerOpts.MaxTxnWaitTime,
	)

	receipt, err := bind.WaitMined(ctx, e.l1Client, tx)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return certHash, fmt.Errorf("cert verification timed out after %v minutes waiting for tx %s to be mined", registerSignerOpts.MaxTxnWaitTime, tx.Hash().Hex())
		}
		return certHash, err
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return certHash, errors.New("cert transaction failed")
	}

	var verified bool

	if serviceType == CaffNode {
		contract, err := espressogen.NewEspressoNitroTEEVerifier(e.address, e.l1Client)
		if err != nil {
			return certHash, err
		}
		// Make sure certificate is verified, after tx succeeded this should always be the case
		// Add retries in case of delay on chain
		verified, err = ContractVerification(
			registerSignerOpts.MaxRetries,
			registerSignerOpts.RetryReadContractDelay,
			func() (bool, error) {
				return contract.CertVerified(&bind.CallOpts{}, certHash)
			},
			"attestation certificate is not yet verified",
		)
		if err != nil {
			return certHash, err
		}
	} else {
		contract, err := legacy_espressogen.NewEspressoNitroTEEVerifier(e.address, e.l1Client)
		if err != nil {
			return certHash, err
		}
		// Make sure certificate is verified, after tx succeeded this should always be the case
		// Add retries in case of delay on chain
		verified, err = ContractVerification(
			registerSignerOpts.MaxRetries,
			registerSignerOpts.RetryReadContractDelay,
			func() (bool, error) {
				return contract.CertVerified(&bind.CallOpts{}, certHash)
			},
			"attestation certificate is not yet verified",
		)
		if err != nil {
			return certHash, err
		}
	}
	if verified {
		log.Info("cert verified", "cert hash", certHash, "isCA", isCA)
		return certHash, nil
	} else {
		return certHash, errors.New("attestation certificate is not registered in contract even after successful transaction")
	}
}
