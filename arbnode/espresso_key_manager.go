package arbnode

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"errors"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/solgen/go/mocksgen"
	"github.com/offchainlabs/nitro/util/signature"
)

type EspressoKeyManagerInterface interface {
	HasRegistered() bool
	Register(signFunc func([]byte) ([]byte, error)) error
	GetCurrentKey() []byte
	SignHotShotPayload(message []byte) ([]byte, error)
	SignBatch(message []byte) ([]byte, error)
}

var _ EspressoKeyManagerInterface = &EspressoKeyManager{}

type EspressoTEEVerifierInterface interface {
	RegisterSigner(opts *bind.TransactOpts, attestation []byte, pubKey []byte, teeType uint8) error
}

type EspressoTEEVerifier struct {
	contract *mocksgen.EspressoTEEVerifierMock
	l1Client *ethclient.Client
}

func NewEspressoTEEVerifier(contract *mocksgen.EspressoTEEVerifierMock, l1Client *ethclient.Client) *EspressoTEEVerifier {
	return &EspressoTEEVerifier{contract: contract, l1Client: l1Client}
}

func (e *EspressoTEEVerifier) RegisterSigner(opts *bind.TransactOpts, attestation []byte, pubKey []byte, teeType uint8) error {
	tx, err := e.contract.RegisterSigner(opts, attestation, pubKey, teeType)
	if err != nil {
		return err
	}

	err = e.l1Client.SendTransaction(context.Background(), tx)
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

	log.Info("Register signer tx succeeded", "tx", tx.Hash())

	return nil
}

type EspressoKeyManager struct {
	espressoTEEVerifierCaller EspressoTEEVerifierInterface
	pubKey                    []byte
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
	pubKeyBytes := crypto.FromECDSAPub(pubKey)

	return &EspressoKeyManager{
		pubKey:                    pubKeyBytes,
		privKey:                   privKey,
		batchPosterSigner:         opts.DataSigner,
		espressoTEEVerifierCaller: espressoTEEVerifierCaller,
		batchPosterOpts:           opts.TransactOpts,
	}
}

func (k *EspressoKeyManager) HasRegistered() bool {
	// TODO: Check address in mock contract
	// pubKey, ok := k.privKey.Public().(*ecdsa.PublicKey)
	// if !ok {
	// 	panic("failed to get public key")
	// }
	// signerAddr := crypto.PubkeyToAddress(*pubKey)
	// err := k.espressoTEEVerifierCaller.registeredSigners(&bind.CallOpts{}, signerAddr, sgx)
	return k.hasRegistered
}

func (k *EspressoKeyManager) Register(signFunc func([]byte) ([]byte, error)) error {
	if k.hasRegistered {
		log.Info("EspressoKeyManager already registered")
		return nil
	}

	attestation, err := signFunc(k.pubKey)
	if err != nil {
		return err
	}

	err = k.espressoTEEVerifierCaller.RegisterSigner(k.batchPosterOpts, attestation, k.pubKey, 0)
	if err != nil {
		return err
	}

	k.hasRegistered = true
	return nil
}

func (k *EspressoKeyManager) GetCurrentKey() []byte {
	return k.pubKey
}

func (k *EspressoKeyManager) SignHotShotPayload(message []byte) ([]byte, error) {
	hash := crypto.Keccak256Hash(message)
	return k.batchPosterSigner(hash.Bytes())
}

func (k *EspressoKeyManager) SignBatch(message []byte) ([]byte, error) {
	hash := crypto.Keccak256Hash(message)
	return k.privKey.Sign(rand.Reader, hash.Bytes(), nil)
}
