package arbnode

import (
	"crypto/ecdsa"
	"crypto/rand"
	"errors"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"

  "github.com/offchainlabs/nitro/espressotee"
	"github.com/offchainlabs/nitro/util/signature"
)

type EspressoKeyManagerInterface interface {
	HasRegistered() (bool, error)
	Register(signFunc func([]byte) ([]byte, error)) error
	GetCurrentKey() *ecdsa.PublicKey
  GetAddress() common.Address
	SignHotShotPayload(message []byte) ([]byte, error)
	SignBatch(message []byte) ([]byte, error)
}
func (e *EspressoTEEVerifier) Verify(opts *bind.TransactOpts, userDataHash []byte, reportDataHash[32]byte) error{
  tx, err := e.contract.Verify(opts, userDataHash, reportDataHash)
  if err != nil{
    return err
  }
	receipt, err := bind.WaitMined(context.Background(), e.l1Client, tx)
	if err != nil {
		return err
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return errors.New("transaction failed")
	}
  
  return nil
}

type EspressoKeyManager struct {
	espressoTEEVerifierCaller espressotee.EspressoTEEVerifierInterface
	pubKey                    *ecdsa.PublicKey
	privKey                   *ecdsa.PrivateKey
  address                   *common.Address

	batchPosterOpts   *bind.TransactOpts
	batchPosterSigner signature.DataSignerFunc

	hasRegistered bool
}

func NewEspressoKeyManager(espressoTEEVerifierCaller espressotee.EspressoTEEVerifierInterface, opts *BatchPosterOpts) *EspressoKeyManager {
	// ephemeral key
	privKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	pubKey, ok := privKey.Public().(*ecdsa.PublicKey)
	if !ok {
		panic("failed to get public key")
	}

  address := crypto.PubkeyToAddress(*pubKey)

	if opts.TransactOpts == nil {
		panic("TransactOpts is nil")
	}

	if opts.DataSigner == nil {
		panic("DataSigner is nil")
	}

	return &EspressoKeyManager{
		pubKey:                    pubKey,
		privKey:                   privKey,
    address:                   &address,
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
	ok, err := k.espressoTEEVerifierCaller.RegisteredSigners(signerAddr)
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

	err = k.espressoTEEVerifierCaller.RegisterSigner(k.batchPosterOpts, attestation, addrBytes)
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

func (k *EspressoKeyManager) GetAddress() common.Address {
	return *k.address
}

func (k *EspressoKeyManager) SignHotShotPayload(message []byte) ([]byte, error) {
	hash := crypto.Keccak256Hash(message)
	return k.batchPosterSigner(hash.Bytes())
}

func (k *EspressoKeyManager) SignBatch(message []byte) ([]byte, error) {
	hash := crypto.Keccak256Hash(message)
	return crypto.Sign(hash.Bytes(), k.privKey)
}
