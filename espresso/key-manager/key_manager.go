package keymanager

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/hf/nsm"
	"github.com/hf/nsm/request"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbnode/dataposter"
	"github.com/offchainlabs/nitro/arbutil"
	espresso_tee_utils "github.com/offchainlabs/nitro/cmd/util/espresso-tee-utils"
	attestationverifierclient "github.com/offchainlabs/nitro/espresso/attestation_verifier_client"
	"github.com/offchainlabs/nitro/espressotee"
	"github.com/offchainlabs/nitro/util/signature"
)

const (
	SGX   = espressotee.SGX
	NITRO = espressotee.NITRO
	TESTS = espressotee.TESTS
	EMPTY = espressotee.EMPTY
)

var FatalErrUnableToRegisterSigner = errors.New("unable to register signer")

type KeyManagerState int

const (
	Init KeyManagerState = iota
	PendingRegistration
	Registered
)

type state struct {
	currentState KeyManagerState
	attestation  []byte
	data         []byte
}

// This is a private key derived from the test test test ... test junk BIP-39 mnemonic. It is a well known private key, so it should be fine to hardcode for tests.
// I found it here: https://ethereum.stackexchange.com/questions/147078/hardhat-which-file-is-initial-state-in-such-as-the-mnemonic-and-20-accounts
const TEST_PERSISTENT_KEY = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
const quoteFile = "/dev/attestation/quote"
const userDataAttestationFile = "/dev/attestation/user_report_data"

type EspressoKeyManagerInterface interface {
	RegisterService() error
	Init() error
	GetCurrentKey() *ecdsa.PublicKey
	SignPayload(message []byte) ([]byte, error)
	SignMessage(message []byte) ([]byte, error)
	TeeType() espressotee.TEE
	GetKeyManagerState() KeyManagerState
	CheckRegistration() (bool, error)
	InitRegistration(getAttestationFunc func([]byte) ([]byte, error)) error
}

var _ EspressoKeyManagerInterface = &EspressoKeyManager{}

type EspressoKeyManager struct {
	espressoTEEVerifierCaller espressotee.EspressoTEEVerifierInterface
	privKey                   *ecdsa.PrivateKey

	signer      signature.DataSignerFunc
	dataPoster  *dataposter.DataPoster
	teeType     espressotee.TEE
	serviceType espressotee.ServiceType

	espressoNitroAttestationVerifierClient *attestationverifierclient.EspressoAttestationVerifierClient
	state                                  state
}

func NewEspressoKeyManager(
	espressoTEEVerifierCaller espressotee.EspressoTEEVerifierInterface,
	dataPoster *dataposter.DataPoster,
	signerFunc signature.DataSignerFunc,
	teeType espressotee.TEE,
	serviceType espressotee.ServiceType,
	servicePersistentPrivateKey *ecdsa.PrivateKey,
	zkAttestationServiceURL string,
	keyPairAttestationsPath string,
	chainID uint64,
) *EspressoKeyManager {
	var err error
	var privKey *ecdsa.PrivateKey

	// Both the caff node and batch poster support persistent private keys. If an existing private key
	// is provided, we use that one. Otherwise, we read the enclave private key from the attestation path.
	// Note: The current implementation only supports reading the key during key manager construction
	// for the batch poster. Support for reading the caff node key will be added in a later PR.
	if keyPairAttestationsPath != "" && chainID != 0 {
		// Read enclave private key
		privKey, err = espresso_tee_utils.ReadEnclavePrivateKey(keyPairAttestationsPath, chainID)
		if err != nil {
			log.Crit("error reading enclave private key for Espresso Key Manager", "path", keyPairAttestationsPath, "err", err)
		}

	} else if servicePersistentPrivateKey != nil {
		privKey = servicePersistentPrivateKey
	} else if teeType == espressotee.TESTS {
		privKey, err = crypto.HexToECDSA(TEST_PERSISTENT_KEY)
		if err != nil {
			log.Crit("Failed to create persistent private key for tests", "err", err)
		}
	} else {
		panic("either keyPairAttestationsPath and chainID must be provided, or servicePersistentPrivateKey must be non-nil")
	}

	// Currently the caff node will not need to sign any payloads, so we check if the service type is a caff node
	// and if it is we can safely ignore a nil data signer.
	if signerFunc == nil && serviceType != espressotee.CaffNode {
		panic("DataSigner is nil")
	}

	if teeType == NITRO && zkAttestationServiceURL == "" {
		if serviceType != espressotee.Test {
			panic("zk attestation service URL must be provided for nitro TEE type")
		} else {
			log.Info("Allowing nitro key manager creation without zkAttestationServiceURL for tests")
		}
	}

	espressoNitroAttestationVerifierClient := attestationverifierclient.NewEspressoAttestationVerifierClient(zkAttestationServiceURL)

	return &EspressoKeyManager{
		privKey:                                privKey,
		signer:                                 signerFunc,
		espressoTEEVerifierCaller:              espressoTEEVerifierCaller,
		dataPoster:                             dataPoster,
		teeType:                                teeType,
		serviceType:                            serviceType,
		espressoNitroAttestationVerifierClient: espressoNitroAttestationVerifierClient,
		state: state{
			currentState: Init,
			attestation:  []byte{},
			data:         []byte{},
		},
	}
}

func (k *EspressoKeyManager) hasRegistered() bool {
	return k.state.currentState == Registered
}

func (k *EspressoKeyManager) GetKeyManagerState() KeyManagerState {
	return k.state.currentState
}

func (k *EspressoKeyManager) VerifyRegistered() (bool, error) {
	if k.hasRegistered() {
		return true, nil
	}
	pubKey, ok := k.privKey.Public().(*ecdsa.PublicKey)
	if !ok {
		panic("failed to get public key")
	}
	signerAddr := crypto.PubkeyToAddress(*pubKey)
	ok, err := k.espressoTEEVerifierCaller.RegisteredServices(signerAddr, uint8(k.teeType), k.serviceType)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (k *EspressoKeyManager) CheckRegistration() (bool, error) {
	state := k.state.currentState
	switch state {
	case Init:
		log.Warn("ephemeral keys are not yet registered in Espresso TEE Contract, KeyManager in Init phase. Waiting for Zk proof to be generated")
		err := k.Init()
		if err != nil {
			return false, fmt.Errorf("unable to init keymanager: %w", err)
		}
		return false, nil
	case PendingRegistration:
		log.Warn("ephemeral keys are not yet registered in Espresso TEE Contract, KeyManager in Registration phase")
		err := k.RegisterService()
		if err != nil {
			return false, fmt.Errorf("%w: %w", FatalErrUnableToRegisterSigner, err)
		}
		return false, nil
	case Registered:
		return true, nil
	default:
		return false, fmt.Errorf("key manager in an unknown state: %v", state)
	}
}

/*
 * This function will get the attestation in order to properly register the signing address on chain for a given TEE type
 */
func (k *EspressoKeyManager) PrepareRegisterService(getAttestationFunc func([]byte) ([]byte, error)) ([]byte, []byte, error) {
	pubKey := k.privKey.PublicKey
	signerAddr := crypto.PubkeyToAddress(pubKey)
	switch k.teeType {
	case SGX:
		addr := signerAddr.Bytes()
		log.Info("sgx signing address", "addr", signerAddr)

		attestationQuote, err := getAttestationFunc(addr)
		if err != nil {
			return nil, nil, fmt.Errorf("sgx signing failed: %w", err)
		}

		return attestationQuote, addr, nil

	case NITRO:
		pubKeyBytes := crypto.FromECDSAPub(&pubKey)
		log.Info("nitro signing address", "addr", signerAddr)

		attestationBytes, err := getAttestationFunc(pubKeyBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("nitro signing failed: %w", err)
		}
		if k.espressoNitroAttestationVerifierClient == nil {
			return nil, nil, errors.New("attestation verifier client is not initialized")
		}
		onchainProof, err := k.espressoNitroAttestationVerifierClient.GenerateZKProof(context.Background(), attestationBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to generate zk proof from nitro attestation: %w", err)
		}

		journalBytes, err := hex.DecodeString(arbutil.StripHexPrefix(onchainProof.RawProof.Journal))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decode journal hex string: %w", err)
		}
		onchainProofBytes, err := hex.DecodeString(arbutil.StripHexPrefix(onchainProof.OnchainProof))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decode onchain proof hex string: %w", err)
		}
		log.Info("successfully generated zk proof from nitro attestation")
		return journalBytes, onchainProofBytes, nil
	case TESTS:
		pubKey := crypto.FromECDSAPub(&k.privKey.PublicKey)
		log.Info("TESTS signing address", "addr", signerAddr)

		attestationQuote, err := getAttestationFunc(pubKey)
		if err != nil {
			return nil, nil, fmt.Errorf("TESTS signing failed: %w", err)
		}
		return attestationQuote, signerAddr.Bytes(), nil
	default:
		return nil, nil, fmt.Errorf("unsupported TEE type: %v", k.teeType)
	}
}

func (k *EspressoKeyManager) InitRegistration(getAttestationFunc func([]byte) ([]byte, error)) error {
	if k.hasRegistered() {
		log.Info("EspressoKeyManager already registered")
		return nil
	}

	// Check on-chain if we have already registered
	hasRegistered, err := k.VerifyRegistered()
	if err != nil {
		return err
	}
	if hasRegistered {
		k.state.currentState = Registered
		k.state.attestation = []byte{}
		k.state.data = []byte{}
		signerAddr := crypto.PubkeyToAddress(k.privKey.PublicKey)
		log.Info("Signer already registered on-chain", "signer address", signerAddr.Hex())
		return nil
	}

	// Get the attestation and data needed to register the signer
	attestation, data, err := k.PrepareRegisterService(getAttestationFunc)
	if err != nil {
		return err
	}
	k.state.currentState = PendingRegistration
	k.state.attestation = attestation
	k.state.data = data
	return nil
}

func (k *EspressoKeyManager) RegisterService() error {
	currentState := k.GetKeyManagerState()
	if currentState != PendingRegistration {
		return fmt.Errorf("invalid state to register signer: got %v, want PendingRegistration", currentState)
	}
	err := k.espressoTEEVerifierCaller.RegisterService(k.dataPoster, k.state.attestation, k.state.data, uint8(k.teeType), k.serviceType)
	if err != nil {
		return err
	}

	signerAddr := crypto.PubkeyToAddress(k.privKey.PublicKey)
	log.Info("Register signer transaction sent", "signer address", signerAddr.Hex())

	// Verify our address is actually registered in contract
	hasRegistered, err := k.VerifyRegistered()
	if err != nil {
		return err
	}
	if !hasRegistered {
		return errors.New("address is not registered in contract even after successful transaction and retries")
	}

	// We are registered free up the memory
	k.state.currentState = Registered
	k.state.attestation = []byte{}
	k.state.data = []byte{}
	log.Info("Signer registration confirmed on-chain")
	return nil
}

func (k *EspressoKeyManager) GetCurrentKey() *ecdsa.PublicKey {
	return &k.privKey.PublicKey
}

func (k *EspressoKeyManager) TeeType() espressotee.TEE {
	return k.teeType
}

func (k *EspressoKeyManager) SignPayload(message []byte) ([]byte, error) {
	return k.signer(crypto.Keccak256Hash(message).Bytes())
}

// SignMessage uses the ephemeral/persistent private key which is generated inside the TEE to sign the given message
func (k *EspressoKeyManager) SignMessage(message []byte) ([]byte, error) {
	return arbutil.SignMessage(message, k.privKey)
}

func (k *EspressoKeyManager) Init() error {
	teeType := k.TeeType()
	switch teeType {
	case SGX:
		return k.InitRegistration(k.getAttestationQuote)
	case NITRO:
		return k.InitRegistration(k.getNitroAttestation)
	case TESTS:
		return k.InitRegistration(k.noOpSignerFunc)
	default:
		return fmt.Errorf("unsupported tee Type: %d", teeType)
	}
}

// getAttestationQuote is a method that retrieves the attestation quote for the user data.
// This function generates the attestation quote for the user data.
// The user data is hashed using keccak256 and then 32 bytes of padding is added to the hash.
// The hash is then written to a file specified in the config. (For SGX: /dev/attestation/user_report_data)
// The quote is then read from the file specified in the config. (For SGX: /dev/attestation/quote)
func (k *EspressoKeyManager) getAttestationQuote(userData []byte) ([]byte, error) {

	// keccak256 hash of userData
	userDataHash := crypto.Keccak256(userData)

	// Add 32 bytes of padding to the user data hash
	// because keccak256 hash is 32 bytes and sgx requires 64 bytes of user data
	for i := 0; i < 32; i += 1 {
		userDataHash = append(userDataHash, 0)
	}

	// Write the message to "/dev/attestation/user_report_data" in SGX
	err := os.WriteFile(userDataAttestationFile, userDataHash, 0600)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to create user report data file: %w", err)
	}

	// Read the quote from "/dev/attestation/quote" in SGX
	attestationQuote, err := os.ReadFile(quoteFile)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to read quote file: %w", err)
	}

	return attestationQuote, nil
}

// getNitroAttestation is a method that retrieves the attestation document for
// AWS Nitro Enclaves.
// This function gets the attestation document for AWS Nitro Enclaves
// We retrieve the Attestation using our epheremal public key we created in EspressoKeyManager
// After we retrieve, we verify the attestation, where we retrieve the result
// Which will contain the complete attestation which we serialize for further processing
func (k *EspressoKeyManager) getNitroAttestation(pubKey []byte) ([]byte, error) {

	sess, err := nsm.OpenDefaultSession()
	if err != nil {
		return nil, fmt.Errorf("failed to open nsm session: %w", err)
	}
	defer sess.Close()

	res, err := sess.Send(&request.Attestation{
		PublicKey: pubKey,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to send attestation request: %w", err)
	}

	if res.Error != "" {
		return nil, fmt.Errorf("nsm returned error: %s", res.Error)
	}

	if res.Attestation == nil || res.Attestation.Document == nil {
		return nil, fmt.Errorf("no attestation document returned")
	}

	return res.Attestation.Document, nil
}

// No-Op Signauture
// This is a function designed to replace a signing function for functionality that depends on operating in a TEE
func (k *EspressoKeyManager) noOpSignerFunc(payload []byte) ([]byte, error) {
	return []byte{}, nil
}
