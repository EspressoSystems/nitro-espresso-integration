package arbnode

import (
	"crypto/ecdsa"
	"crypto/rand"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
)

type EspressoKeyManagerInterface interface {
	HasRegistered() bool
	Registry(hotshotBlock uint64, signFunc func([]byte) ([]byte, error)) error
	GetCurrentKey() []byte
	Sign(message []byte) ([]byte, error)
}

var _ EspressoKeyManagerInterface = &EspressoKeyManager{}

type EspressoKeyManager struct {
	address common.Address
	pubKey  []byte
	privKey *ecdsa.PrivateKey

	hasRegistered bool
	client        *ethclient.Client
}

func NewEspressoKeyManager(registryAddr common.Address, client *ethclient.Client) *EspressoKeyManager {
	privKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	pubKey, ok := privKey.Public().(*ecdsa.PublicKey)
	if !ok {
		panic("failed to get public key")
	}
	pubKeyBytes := crypto.CompressPubkey(pubKey)

	return &EspressoKeyManager{
		address: registryAddr,
		pubKey:  pubKeyBytes,
		privKey: privKey,
		client:  client,
	}
}

func (k *EspressoKeyManager) HasRegistered() bool {
	return k.hasRegistered
}

func (k *EspressoKeyManager) Registry(hotshotBlock uint64, signFunc func([]byte) ([]byte, error)) error {
	if k.hasRegistered {
		log.Info("EspressoKeyManager already registered")
		return nil
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
	return k.privKey.Sign(rand.Reader, hash.Bytes(), nil)
}
