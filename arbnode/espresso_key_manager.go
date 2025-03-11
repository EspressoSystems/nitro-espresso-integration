package arbnode

import (
	"crypto/ecdsa"
	"crypto/rand"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

type EspressoKeyManagerInterface interface {
	HasRegistered() bool
	Register(signFunc func([]byte) ([]byte, error)) error
	GetCurrentKey() []byte
	Sign(message []byte) ([]byte, error)
}

var _ EspressoKeyManagerInterface = &EspressoKeyManager{}

type EspressoTEEVerifierInterface interface {
	Verify(opts *bind.CallOpts, rawQuote []byte, reportDataHash [32]byte) error
}

type EspressoKeyManager struct {
	espressoTEEVerifierCaller EspressoTEEVerifierInterface
	pubKey                    []byte
	privKey                   *ecdsa.PrivateKey
	batchPosterPrivKey        *ecdsa.PrivateKey

	hasRegistered bool
}

func NewEspressoKeyManager(espressoTEEVerifierCaller EspressoTEEVerifierInterface, batchPosterPrivKey string) *EspressoKeyManager {
	// ephemeral key
	privKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	pubKey, ok := privKey.Public().(*ecdsa.PublicKey)
	if !ok {
		panic("failed to get public key")
	}
	pubKeyBytes := crypto.CompressPubkey(pubKey)

	// batch poster private key
	keyBytes, err := hexutil.Decode("0x" + batchPosterPrivKey)
	if err != nil {
		panic("failed to decode")
	}
	batchPosterKey, ecdsaErr := crypto.ToECDSA(keyBytes)
	if ecdsaErr != nil {
		panic("failed to convert")
	}

	return &EspressoKeyManager{
		pubKey:                    pubKeyBytes,
		privKey:                   privKey,
		batchPosterPrivKey:        batchPosterKey,
		espressoTEEVerifierCaller: espressoTEEVerifierCaller,
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

	// TODO: Register address in mock contract
	// pubKey, ok := k.privKey.Public().(*ecdsa.PublicKey)
	// if !ok {
	// 	panic("failed to get public key")
	// }
	// signerAddr := crypto.PubkeyToAddress(*pubKey)
	// err := k.espressoTEEVerifierCaller.registerSigner(&bind.CallOpts{}, attestation, signerAddr, sgx)
	var quote []byte
	var hash [32]byte
	teeErr := k.espressoTEEVerifierCaller.Verify(&bind.CallOpts{}, quote, hash)
	if teeErr != nil {
		return teeErr
	}

	_, err := signFunc(k.pubKey)
	if err != nil {
		return err
	}

	k.hasRegistered = true
	return nil
}

func (k *EspressoKeyManager) GetCurrentKey() []byte {
	return k.pubKey
}

func (k *EspressoKeyManager) Sign(message []byte) ([]byte, error) {
	hash := crypto.Keccak256Hash(message)
	return k.batchPosterPrivKey.Sign(rand.Reader, hash.Bytes(), nil)
}
