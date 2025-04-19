package espressotee

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/hf/nitrite"
	"github.com/offchainlabs/nitro/solgen/go/espressogen"
)

type EspressoNitroTEEVerifierInterface interface {
	VerifyCert(opts *bind.TransactOpts, certificate []byte, parentCertHash [32]byte, isCA bool) (common.Hash, error)
	VerifyAttestationCertificates(attestationBytes []byte, opts *bind.TransactOpts) ([]byte, []byte, error)
}

type EspressoNitroTEEVerifier struct {
	contract *espressogen.IEspressoNitroTEEVerifier
	l1Client *ethclient.Client
}

func NewEspressoNitroTEEVerifier(contract *espressogen.IEspressoNitroTEEVerifier, l1Client *ethclient.Client) *EspressoNitroTEEVerifier {
	return &EspressoNitroTEEVerifier{contract: contract, l1Client: l1Client}
}

/**
 * This functions checks and verifies a certificate on-chain.
 * It first checks if the certificate is already verified by its hash. If not, it submits a verification transaction.
 */
func (e *EspressoNitroTEEVerifier) VerifyCert(opts *bind.TransactOpts, certificate []byte, parentCertHash [32]byte, isCA bool) (common.Hash, error) {
	// Get certificate hash, see and see if its already verified on chain
	certHash := crypto.Keccak256Hash(certificate)
	verified, err := e.contract.CertVerified(&bind.CallOpts{}, certHash)
	if err != nil {
		return certHash, err
	}
	if verified {
		log.Info("cert already verified", "cert hash", certHash, "isCA", isCA)
		return certHash, nil
	}

	// If not verified, try and verify the certificate
	tx, err := e.contract.VerifyCert(opts, certificate, parentCertHash, isCA)
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
 * 1. The CA certificate chain
 * 2. The client certificate
 */
func (e *EspressoNitroTEEVerifier) VerifyAttestationCertificates(attestationBytes []byte, opts *bind.TransactOpts) (attestation, data []byte, err error) {
	// Unmarshal attestation document
	var res nitrite.Result
	err = json.Unmarshal(attestationBytes, &res)
	if err != nil {
		return nil, nil, err
	}

	log.Info("successfully got attestation", "pcr0 hash", "0x"+hex.EncodeToString(crypto.Keccak256(res.Document.PCRs[0])))

	// Verify CA certificate chain
	if len(res.Document.CABundle) == 0 {
		return nil, nil, errors.New("CA bundle is empty")
	}

	// Go over the CA certificate bundle in attestation and verify each
	parentCertHash := crypto.Keccak256Hash(res.Document.CABundle[0])
	for i := 0; i < len(res.Document.CABundle); i++ {
		cert := res.Document.CABundle[i]
		// Verify current certificate against parent hash in NitroEspressoTEEVerifier contracts
		certHash, err := e.VerifyCert(opts, cert, parentCertHash, true)
		if err != nil {
			log.Error("failed to get CA cert verified", "index", i, "err", err)
			return nil, nil, err
		}

		// For next certificate in bundle we need to compare it with this certificate hash
		parentCertHash = certHash
	}

	// Verify client certificate
	_, err = e.VerifyCert(opts, res.Document.Certificate, parentCertHash, false)
	if err != nil {
		log.Error("failed to get client cert verified", "err", err)
		return nil, nil, err
	}

	// Return attestation and signature
	return res.COSESign1, res.Signature, nil
}
