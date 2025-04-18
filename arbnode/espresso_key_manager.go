package arbnode

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/espressotee"
	"github.com/offchainlabs/nitro/solgen/go/espressogen"
	"github.com/offchainlabs/nitro/util/signature"
)

type EspressoKeyManagerInterface interface {
	HasRegistered() (bool, error)
	Register(getAttestationFunc func([]byte) ([]byte, error)) error
	GetCurrentKey() *ecdsa.PublicKey
	SignHotShotPayload(message []byte) ([]byte, error)
	SignBatch(message []byte) ([]byte, error)
	TeeType() TEE
}

var _ EspressoKeyManagerInterface = &EspressoKeyManager{}

type EspressoTEEVerifierInterface interface {
	RegisterSigner(opts *bind.TransactOpts, attestation []byte, data []byte, teeType uint8) (common.Hash, error)
	RegisteredSigners(signer common.Address, teeType uint8) (bool, error)
}

type EspressoTEEVerifier struct {
	contract *espressogen.IEspressoTEEVerifier
	l1Client *ethclient.Client
}

func NewEspressoTEEVerifier(contract *espressogen.IEspressoTEEVerifier, l1Client *ethclient.Client) *EspressoTEEVerifier {
	return &EspressoTEEVerifier{contract: contract, l1Client: l1Client}
}

func (e *EspressoTEEVerifier) RegisterSigner(opts *bind.TransactOpts, attestation []byte, data []byte, teeType uint8) (common.Hash, error) {
	tx, err := e.contract.RegisterSigner(opts, attestation, data, teeType)
	if err != nil {
		return common.Hash{}, err
	}

	log.Info("Waiting for register signer tx to be mined", "tx", tx.Hash())

	receipt, err := bind.WaitMined(context.Background(), e.l1Client, tx)
	if err != nil {
		return common.Hash{}, err
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return common.Hash{}, errors.New("transaction failed")
	}

	return tx.Hash(), nil
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
	espressoNitroTEEVerifier  espressotee.EspressoNitroTEEVerifierInterface
	pubKey                    *ecdsa.PublicKey
	privKey                   *ecdsa.PrivateKey

	batchPosterOpts   *bind.TransactOpts
	batchPosterSigner signature.DataSignerFunc
	teeType           TEE

	hasRegistered bool
}

func NewEspressoKeyManager(espressoTEEVerifierCaller EspressoTEEVerifierInterface, espressoNitroTEEVerifier espressotee.EspressoNitroTEEVerifierInterface, opts *BatchPosterOpts, teeType TEE) *EspressoKeyManager {
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
		espressoNitroTEEVerifier:  espressoNitroTEEVerifier,
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

/*
 * This function will get the attestation in order to properly register the signing address on chain for a given TEE type
 */
func (k *EspressoKeyManager) PrepareRegisterSigner(getAttestationFunc func([]byte) ([]byte, error)) ([]byte, []byte, common.Address, error) {
	signerAddr := crypto.PubkeyToAddress(*k.pubKey)
	switch k.teeType {
	case SGX:
		addr := signerAddr.Bytes()
		log.Info("sgx signing address", "addr", signerAddr)

		attestationQuote, err := getAttestationFunc(addr)
		if err != nil {
			return nil, nil, common.Address{}, fmt.Errorf("sgx signing failed: %w", err)
		}
		return attestationQuote, addr, signerAddr, nil

	case NITRO:
		pubKeyBytes := crypto.FromECDSAPub(k.pubKey)
		log.Info("nitro signing address", "addr", signerAddr)

		attestationBytes, err := getAttestationFunc(pubKeyBytes)
		if err != nil {
			return nil, nil, common.Address{}, fmt.Errorf("nitro signing failed: %w", err)
		}

		attestation, data, err := k.espressoNitroTEEVerifier.VerifyAttestationCertificates(
			attestationBytes,
			k.batchPosterOpts,
		)
		if err != nil {
			return nil, nil, common.Address{}, fmt.Errorf("attestation verification failed: %w", err)
		}
		return attestation, data, signerAddr, nil

	default:
		return nil, nil, common.Address{}, fmt.Errorf("unsupported TEE type: %v", k.teeType)
	}
}

func (k *EspressoKeyManager) Register(getAttestationFunc func([]byte) ([]byte, error)) error {
	if k.hasRegistered {
		log.Info("EspressoKeyManager already registered")
		return nil
	}

	// Get the attestation and data needed to register the signer
	attestation, data, signerAddr, err := k.PrepareRegisterSigner(getAttestationFunc)
	if err != nil {
		return err
	}

	txHash, err := k.espressoTEEVerifierCaller.RegisterSigner(k.batchPosterOpts, attestation, data, uint8(k.teeType))
	if err != nil {
		return err
	}

	log.Info("Register signer tx succeeded", "signer address", signerAddr, "tx", txHash)

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
