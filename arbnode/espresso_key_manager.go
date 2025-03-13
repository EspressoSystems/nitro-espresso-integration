package arbnode

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"errors"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/solgen/go/mocksgen"
	"github.com/offchainlabs/nitro/util/signature"
)

type EspressoKeyManagerInterface interface {
	HasRegistered() (bool, error)
	Register(signFunc func([]byte) ([]byte, error)) error
	GetCurrentKey() *ecdsa.PublicKey
	SignHotShotPayload(message []byte) ([]byte, error)
	SignBatch(message []byte) ([]byte, error)
}

var _ EspressoKeyManagerInterface = &EspressoKeyManager{}

type EspressoTEEVerifierInterface interface {
	RegisterSigner(opts *bind.TransactOpts, attestation []byte, addr []byte, teeType uint8) error
	RegisteredSigners(signer common.Address, teeType uint8) (bool, error)
}

type EspressoTEEVerifier struct {
	contract *mocksgen.EspressoTEEVerifierMock
	l1Client *ethclient.Client
}

func NewEspressoTEEVerifier(contract *mocksgen.EspressoTEEVerifierMock, l1Client *ethclient.Client) *EspressoTEEVerifier {
	return &EspressoTEEVerifier{contract: contract, l1Client: l1Client}
}

func (e *EspressoTEEVerifier) RegisterSigner(opts *bind.TransactOpts, attestation []byte, addr []byte, teeType uint8) error {
	tx, err := e.contract.RegisterSigner(opts, attestation, addr, teeType)
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

	log.Info("Register signer tx succeeded", "signer address", addr, "tx", tx.Hash())

	return nil
}

func (e *EspressoTEEVerifier) RegisteredSigners(address common.Address, teeType uint8) (bool, error) {
	return e.contract.RegisteredSigners(&bind.CallOpts{}, address, teeType)
}

type EspressoKeyManager struct {
	espressoTEEVerifierCaller EspressoTEEVerifierInterface
	pubKey                    *ecdsa.PublicKey
	privKey                   *ecdsa.PrivateKey

	batchPosterOpts   *bind.TransactOpts
	batchPosterSigner signature.DataSignerFunc

	hasRegistered bool
}

func NewEspressoKeyManager(espressoTEEVerifierCaller EspressoTEEVerifierInterface, opts *BatchPosterOpts) *EspressoKeyManager {
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
	}
}

func (k *EspressoKeyManager) HasRegistered() (bool, error) {
	pubKey, ok := k.privKey.Public().(*ecdsa.PublicKey)
	if !ok {
		panic("failed to get public key")
	}
	signerAddr := crypto.PubkeyToAddress(*pubKey)
	ok, err := k.espressoTEEVerifierCaller.RegisteredSigners(signerAddr, 0)
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

	addr := crypto.PubkeyToAddress(*k.pubKey)
	addrBytes := addr.Bytes()

	log.Info("Signing address", "addr", addrBytes)
	attestation, err := signFunc(addrBytes)
	if err != nil {
		return err
	}

	err = k.espressoTEEVerifierCaller.RegisterSigner(k.batchPosterOpts, attestation, addrBytes, 0)
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

func (k *EspressoKeyManager) SignHotShotPayload(message []byte) ([]byte, error) {
	hash := crypto.Keccak256Hash(message)
	return k.batchPosterSigner(hash.Bytes())
}

func (k *EspressoKeyManager) SignBatch(message []byte) ([]byte, error) {
	hash := crypto.Keccak256Hash(message)
	return crypto.Sign(hash.Bytes(), k.privKey)
}
