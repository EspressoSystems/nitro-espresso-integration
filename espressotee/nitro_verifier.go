package espressotee

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/hf/nitrite"
	"github.com/offchainlabs/nitro/arbnode/dataposter"
	"github.com/offchainlabs/nitro/solgen/go/espressogen"
)

type EspressoNitroTEEVerifierInterface interface {
	VerifyCert(opts *bind.TransactOpts, dataPoster *dataposter.DataPoster, nitroAddr common.Address, certificate []byte, parentCertHash [32]byte, isCA bool) (common.Hash, error)
	VerifyAttestationAndCertificates(attestationBytes []byte, opts *bind.TransactOpts, dataPoster *dataposter.DataPoster, nitroAddr common.Address) ([]byte, []byte, error)
	IsPCR0HashRegistered(pcr0Hash [32]byte) (bool, error)
}

type EspressoNitroTEEVerifier struct {
	contract *espressogen.IEspressoNitroTEEVerifier
	l1Client *ethclient.Client
}

func NewEspressoNitroTEEVerifier(contract *espressogen.IEspressoNitroTEEVerifier, l1Client *ethclient.Client) *EspressoNitroTEEVerifier {
	return &EspressoNitroTEEVerifier{contract: contract, l1Client: l1Client}
}

func (e *EspressoNitroTEEVerifier) IsPCR0HashRegistered(pcr0Hash [32]byte) (bool, error) {
	return e.contract.RegisteredEnclaveHash(&bind.CallOpts{}, pcr0Hash)
}

/**
 * This functions checks and verifies a certificate on-chain.
 * It first checks if the certificate is already verified by its hash. If not, it submits a verification transaction.
 */
func (e *EspressoNitroTEEVerifier) VerifyCert(opts *bind.TransactOpts, dataPoster *dataposter.DataPoster, nitroAddr common.Address, certificate []byte, parentCertHash [32]byte, isCA bool) (common.Hash, error) {
	// Get certificate hash, see and see if its already verified on chain
	certHash := crypto.Keccak256Hash(certificate)
	verified, err := e.contract.CertVerified(&bind.CallOpts{}, certHash)
	if err != nil {
		return certHash, err
	}
	if verified {
		log.Info("cert already verified", "cert hash", certHash, "isCA", isCA)
		// return certHash, nil
	}

	// If not verified, try and verify the certificate either CA or client
	contractABI, err := abi.JSON(strings.NewReader(espressogen.IEspressoNitroTEEVerifierMetaData.ABI))
	if err != nil {
		return certHash, err
	}

	// Pack the function arguments (attestation, data, teeType)
	log.Info("gas limit", "limit", opts.GasLimit)
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
		From:  opts.From,
		To:    &nitroAddr,
		Data:  calldata,
		Value: opts.Value,
	}
	estimate, err := e.l1Client.EstimateGas(context.Background(), msg)
	if err != nil {
		return certHash, err
	}
	log.Info("gas limit", "estimate", estimate)
	higher := estimate + 9000000
	tx, err := dataPoster.PostSimpleTransaction(
		context.Background(),
		nitroAddr,
		calldata,
		higher,
		opts.Value,
	)
	if err != nil {
		return certHash, err
	}
	log.Info("Waiting for cert tx to be mined", "tx", tx.Hash(), "isCA", isCA)
	receipt, err := bind.WaitMined(context.Background(), e.l1Client, tx)
	if err != nil {
		return certHash, err
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return certHash, errors.New("cert transaction failed")
	}
	return certHash, nil
}

/**
 * This function validates parses the attestation result we received from AWS Nitro Secure Module (NSM) then validates the following on-chain
 * 1. The PCR0 hash is registered in the contracts
 * 2. The CA certificate chain
 * 3. The client certificate
 */
func (e *EspressoNitroTEEVerifier) VerifyAttestationAndCertificates(attestationBytes []byte, opts *bind.TransactOpts, dataPoster *dataposter.DataPoster, nitroAddr common.Address) (attestation []byte, data []byte, err error) {
	// Unmarshal attestation document
	var res nitrite.Result
	err = json.Unmarshal(attestationBytes, &res)
	if err != nil {
		return nil, nil, err
	}

	pcr0Hash := crypto.Keccak256Hash(res.Document.PCRs[0])
	log.Info("successfully got attestation", "pcr0 hash", pcr0Hash)

	// Before verifying certificates on chain, check if the pcr0 hash is registered to save gas
	verified, err := e.IsPCR0HashRegistered(pcr0Hash)
	if err != nil {
		log.Error("failed to check if pcr0 hash is verified", "pcr0 hash", pcr0Hash)
		return nil, nil, err
	}

	if !verified {
		return nil, nil, fmt.Errorf("prc0 hash is not registered: %x", pcr0Hash)
	}

	// Verify CA certificate chain
	if len(res.Document.CABundle) == 0 {
		return nil, nil, errors.New("CA bundle is empty")
	}

	// Go over the CA certificate bundle in attestation and verify each
	parentCertHash := crypto.Keccak256Hash(res.Document.CABundle[0])
	for i := 0; i < len(res.Document.CABundle); i++ {
		cert := res.Document.CABundle[i]
		// Verify current certificate against parent hash in NitroEspressoTEEVerifier contracts
		certHash, err := e.VerifyCert(opts, dataPoster, nitroAddr, cert, parentCertHash, true)
		if err != nil {
			log.Error("failed to get CA cert verified", "index", i, "err", err)
			return nil, nil, err
		}

		// For next certificate in bundle we need to compare it with this certificate hash
		parentCertHash = certHash
	}

	// Verify client certificate
	_, err = e.VerifyCert(opts, dataPoster, nitroAddr, res.Document.Certificate, parentCertHash, false)
	if err != nil {
		log.Error("failed to get client cert verified", "err", err)
		return nil, nil, err
	}

	// Return attestation and signature
	return res.COSESign1, res.Signature, nil
}
