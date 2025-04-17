package arbnode

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
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
	"github.com/offchainlabs/nitro/util/signature"
)

type EspressoKeyManagerInterface interface {
	HasRegistered() (bool, error)
	Register(signFunc func([]byte) ([]byte, error)) error
	GetCurrentKey() *ecdsa.PublicKey
	SignHotShotPayload(message []byte) ([]byte, error)
	SignBatch(message []byte) ([]byte, error)
	TeeType() TEE
}

var _ EspressoKeyManagerInterface = &EspressoKeyManager{}

type EspressoTEEVerifierInterface interface {
	RegisterSigner(opts *bind.TransactOpts, attestation []byte, data []byte, teeType uint8, signerAddr common.Address) error
	RegisteredSigners(signer common.Address, teeType uint8) (bool, error)
	VerifyCert(opts *bind.TransactOpts, certificate []byte, parentCertHash [32]byte, isCA bool, teeType uint8) (common.Hash, error)
}

type EspressoTEEVerifier struct {
	contract *espressogen.IEspressoTEEVerifier
	l1Client *ethclient.Client
}

func NewEspressoTEEVerifier(contract *espressogen.IEspressoTEEVerifier, l1Client *ethclient.Client) *EspressoTEEVerifier {
	return &EspressoTEEVerifier{contract: contract, l1Client: l1Client}
}

func (e *EspressoTEEVerifier) RegisterSigner(opts *bind.TransactOpts, attestation []byte, data []byte, teeType uint8, signerAddr common.Address) error {
	tx, err := e.contract.RegisterSigner(opts, attestation, data, teeType)
	if err != nil {
		return err
	}

	log.Info("Waiting for register signer tx to be mined", "tx", tx.Hash())

	receipt, err := bind.WaitMined(context.Background(), e.l1Client, tx)
	if err != nil {
		return err
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return errors.New("transaction failed")
	}

	log.Info("Register signer tx succeeded", "signer address", signerAddr, "tx", tx.Hash())

	return nil
}

func (e *EspressoTEEVerifier) VerifyCert(opts *bind.TransactOpts, certificate []byte, parentCertHash [32]byte, isCA bool, teeType uint8) (common.Hash, error) {
	certHash := crypto.Keccak256Hash(certificate)
	verified, err := e.contract.CertVerified(&bind.CallOpts{}, certHash, teeType)
	if verified {
		log.Info("cert already verified", "cert hash", certHash, "isCA", isCA)
		return certHash, nil
	}

	tx, err := e.contract.VerifyCert(opts, certificate, parentCertHash, isCA, teeType)
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

func (e *EspressoTEEVerifier) RegisteredSigners(address common.Address, teeType uint8) (bool, error) {
	return e.contract.RegisteredSigners(&bind.CallOpts{}, address, teeType)
}

type TEE uint8

const (
	SGX   TEE = 0 // SGX
	NITRO TEE = 1 // AWS Nitro
)

type EspressoKeyManager struct {
	espressoTEEVerifierCaller EspressoTEEVerifierInterface
	pubKey                    *ecdsa.PublicKey
	privKey                   *ecdsa.PrivateKey

	batchPosterOpts   *bind.TransactOpts
	batchPosterSigner signature.DataSignerFunc
	teeType           TEE

	hasRegistered bool
}

func NewEspressoKeyManager(espressoTEEVerifierCaller EspressoTEEVerifierInterface, opts *BatchPosterOpts, teeType TEE) *EspressoKeyManager {
	// ephemeral key
	privKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	pubKey, ok := privKey.Public().(*ecdsa.PublicKey)
	if !ok {
		panic("failed to get public key")
	}

	if opts.TransactOpts == nil {
		panic("TransactOpts is nil")
	}

	if opts.DataSigner == nil {
		panic("DataSigner is nil")
	}

	return &EspressoKeyManager{
		pubKey:                    pubKey,
		privKey:                   privKey,
		batchPosterSigner:         opts.DataSigner,
		espressoTEEVerifierCaller: espressoTEEVerifierCaller,
		batchPosterOpts:           opts.TransactOpts,
		teeType:                   teeType,
	}
}

func (k *EspressoKeyManager) HasRegistered() (bool, error) {
	pubKey, ok := k.privKey.Public().(*ecdsa.PublicKey)
	if !ok {
		panic("failed to get public key")
	}
	signerAddr := crypto.PubkeyToAddress(*pubKey)
	ok, err := k.espressoTEEVerifierCaller.RegisteredSigners(signerAddr, uint8(k.teeType))
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (k *EspressoKeyManager) Register(signFunc func([]byte) ([]byte, error)) error {
	if k.hasRegistered {
		log.Info("EspressoKeyManager already registered")
		return nil
	}

	var attestation []byte
	var data []byte
	var signerAddr common.Address
	if k.teeType == SGX {
		addr := crypto.PubkeyToAddress(*k.pubKey)
		data = addr.Bytes()
		signerAddr = addr
		log.Info("sgx signing address", "addr", data)
		res, err := signFunc(data)
		if err != nil {
			return err
		}
		attestation = res

	} else if k.teeType == NITRO {
		pubKeyBytes := crypto.FromECDSAPub(k.pubKey)
		signerAddr = crypto.PubkeyToAddress(*k.pubKey)
		log.Info("nitro signing address", "addr", signerAddr)
		attestationBytes, err := signFunc(pubKeyBytes)
		if err != nil {
			return err
		}

		var res nitrite.Result
		err = json.Unmarshal(attestationBytes, &res)
		if err != nil {
			return err
		}

		log.Info("succesfully got attestation", "pcr0 hash", "0x"+hex.EncodeToString(crypto.Keccak256(res.Document.PCRs[0])))

		// Verify attestation certificates first, to prevent high gas prices in RegisterSigner
		parentCertHash := crypto.Keccak256Hash(res.Document.CABundle[0])
		for i := 0; i < len(res.Document.CABundle); i++ {
			cert := res.Document.CABundle[i]
			certHash, err := k.espressoTEEVerifierCaller.VerifyCert(k.batchPosterOpts, cert, parentCertHash, true, uint8(k.teeType))
			if err != nil {
				log.Error("failed to get ca cert verified")
				return err
			}

			parentCertHash = certHash
		}

		_, err = k.espressoTEEVerifierCaller.VerifyCert(k.batchPosterOpts, res.Document.Certificate, parentCertHash, false, uint8(k.teeType))
		if err != nil {
			log.Error("failed to get client cert verified")
			return err
		}
		attestation = res.COSESign1
		data = res.Signature
	}

	err := k.espressoTEEVerifierCaller.RegisterSigner(k.batchPosterOpts, attestation, data, uint8(k.teeType), signerAddr)
	if err != nil {
		return err
	}

	// Verify our address is actually registered in contract
	hasRegistered, err := k.HasRegistered()
	if err != nil {
		return err
	}
	if !hasRegistered {
		return errors.New("address is not registered in contract")
	}
	k.hasRegistered = true
	return nil
}

func (k *EspressoKeyManager) GetCurrentKey() *ecdsa.PublicKey {
	return k.pubKey
}

func (k *EspressoKeyManager) TeeType() TEE {
	return k.teeType
}

func (k *EspressoKeyManager) SignHotShotPayload(message []byte) ([]byte, error) {
	hash := crypto.Keccak256Hash(message)
	return k.batchPosterSigner(hash.Bytes())
}

func (k *EspressoKeyManager) SignBatch(message []byte) ([]byte, error) {
	hash := crypto.Keccak256Hash(message)
	return crypto.Sign(hash.Bytes(), k.privKey)
}
