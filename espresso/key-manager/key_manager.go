package keymanager

import (
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbnode/dataposter"
	"github.com/offchainlabs/nitro/espresso-tee-contracts/espressogen"
	"github.com/offchainlabs/nitro/espressotee"
	"github.com/offchainlabs/nitro/util/signature"
)

const (
	SGX   = espressotee.SGX
	NITRO = espressotee.NITRO
	TESTS = espressotee.TEETEST
	EMPTY = espressotee.EMPTY
	//
)

type EspressoKeyManagerInterface interface {
	HasRegistered() bool
	Register(getAttestationFunc func([]byte) ([]byte, error)) error
	RegisterService() error
	GetCurrentKey() *ecdsa.PublicKey
	SignHotShotPayload(message []byte) ([]byte, error)
	SignBatch(message []byte) ([]byte, error)
	TeeType() espressotee.TEE
}

var _ EspressoKeyManagerInterface = &EspressoKeyManager{}

type EspressoKeyManager struct {
	espressoTEEVerifierCaller espressotee.EspressoTEEVerifierInterface
	espressoNitroTEEVerifier  espressotee.EspressoNitroTEEVerifierInterface
	pubKey                    *ecdsa.PublicKey
	privKey                   *ecdsa.PrivateKey

	signer                  signature.DataSignerFunc
	dataPoster              *dataposter.DataPoster
	teeType                 espressotee.TEE
	serviceType             espressotee.ServiceType
	registerSignerOpts      espressotee.EspressoRegisterSignerOpts
	userDataAttestationFile string
	quoteFile               string

	hasRegistered bool
}

func NewEspressoKeyManager(
	espressoTEEVerifierCaller espressotee.EspressoTEEVerifierInterface,
	espressoNitroTEEVerifier espressotee.EspressoNitroTEEVerifierInterface,
	dataPoster *dataposter.DataPoster,
	signerFunc signature.DataSignerFunc,
	teeType espressotee.TEE,
	serviceTupe espressotee.ServiceType,
	registerSignerConfig espressotee.EspressoRegisterSignerConfig,
) *EspressoKeyManager {
	// ephemeral key
	privKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	pubKey, ok := privKey.Public().(*ecdsa.PublicKey)
	if !ok {
		panic("failed to get public key")
	}

	if signerFunc == nil {
		panic("DataSigner is nil")
	}

	if registerSignerConfig.GasLimitBufferIncreasePercent > 20 {
		panic("Gas limit buffer increase should not be greater than 20 percent")
	}

	if registerSignerConfig.MaxRetries > 10 {
		panic("Max retries cannot be more than 10")
	}

	if registerSignerConfig.MaxTxnWaitTime > 5*time.Minute {
		panic("Max txn wait time cannot be more than 5 minutes")
	}

	if registerSignerConfig.RetryReadContractDelay > 20*time.Second {
		panic("Retry read contract delay cannot be more than 20 seconds")
	}

	if registerSignerConfig.RetryBaseFeeDelay > 3*time.Minute {
		panic("Retry getting base fee delay cannot be more than 3 minutes")
	}

	return &EspressoKeyManager{
		pubKey:                    pubKey,
		privKey:                   privKey,
		batchPosterSigner:         signerFunc,
		espressoTEEVerifierCaller: espressoTEEVerifierCaller,
		espressoNitroTEEVerifier:  espressoNitroTEEVerifier,
		dataPoster:                dataPoster,
		teeType:                   teeType,
		registerSignerOpts: espressotee.EspressoRegisterSignerOpts{
			MaxTxnWaitTime:                registerSignerConfig.MaxTxnWaitTime,
			MaxRetries:                    int(registerSignerConfig.MaxRetries),
			RetryBaseFeeDelay:             registerSignerConfig.RetryBaseFeeDelay,
			RetryReadContractDelay:        registerSignerConfig.RetryReadContractDelay,
			GasLimitBufferIncreasePercent: registerSignerConfig.GasLimitBufferIncreasePercent,
			MaxBaseFee:                    registerSignerConfig.MaxBaseFee,
		},
	}
}

func (k *EspressoKeyManager) HasRegistered() bool {
	return k.hasRegistered
}

func (k *EspressoKeyManager) VerifyRegistered() (bool, error) {
	if k.hasRegistered {
		return true, nil
	}
	pubKey, ok := k.privKey.Public().(*ecdsa.PublicKey)
	if !ok {
		panic("failed to get public key")
	}
	signerAddr := crypto.PubkeyToAddress(*pubKey)
	ok, err := k.espressoTEEVerifierCaller.RegisteredServices(signerAddr, uint8(k.teeType), espressotee.BatchPoster, k.registerSignerOpts)
	if err != nil {
		return false, err
	}
	return ok, nil
}

/*
 * This function will get the attestation in order to properly register the signing address on chain for a given TEE type
 */
func (k *EspressoKeyManager) PrepareRegisterService(getAttestationFunc func([]byte) ([]byte, error)) ([]byte, []byte, error) {
	signerAddr := crypto.PubkeyToAddress(*k.pubKey)
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
		pubKeyBytes := crypto.FromECDSAPub(k.pubKey)
		log.Info("nitro signing address", "addr", signerAddr)

		attestationBytes, err := getAttestationFunc(pubKeyBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("nitro signing failed: %w", err)
		}

		attestation, data, err := k.espressoNitroTEEVerifier.VerifyAttestationAndCertificates(
			attestationBytes,
			k.dataPoster,
			k.registerSignerOpts,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("attestation verification failed: %w", err)
		}
		return attestation, data, nil
	case TESTS:
		data := crypto.FromECDSAPub(k.pubKey)
		signature, err := k.noOpSignerFunc(data)
		return signature, data, err
	default:
		return nil, nil, fmt.Errorf("unsupported TEE type: %v", k.teeType)
	}
}

func (k *EspressoKeyManager) Register(getAttestationFunc func([]byte) ([]byte, error)) error {
	if k.hasRegistered {
		log.Info("EspressoKeyManager already registered")
		return nil
	}

	// Get the attestation and data needed to register the signer
	attestation, data, err := k.PrepareRegisterService(getAttestationFunc)
	if err != nil {
		return err
	}

	err = k.espressoTEEVerifierCaller.RegisterService(k.dataPoster, attestation, data, uint8(k.teeType), k.serviceType, k.registerSignerOpts)
	if err != nil {
		return err
	}

	signerAddr := crypto.PubkeyToAddress(*k.pubKey)
	log.Info("Register signer transaction sent", "signer address", signerAddr.Hex())

	// Verify our address is actually registered in contract
	hasRegistered, err := k.VerifyRegistered()
	if err != nil {
		return err
	}
	if !hasRegistered {
		return errors.New("address is not registered in contract even after successful transaction and retries")
	}

	k.hasRegistered = true
	log.Info("Signer registration confirmed on-chain")
	return nil
}

func (k *EspressoKeyManager) GetCurrentKey() *ecdsa.PublicKey {
	return k.pubKey
}

func (k *EspressoKeyManager) TeeType() espressotee.TEE {
	return k.teeType
}

func (k *EspressoKeyManager) SignHotShotPayload(message []byte) ([]byte, error) {
	return k.batchPosterSigner(crypto.Keccak256Hash(message).Bytes())
}

func (k *EspressoKeyManager) SignBatch(message []byte) ([]byte, error) {
	hash := crypto.Keccak256Hash(message)
	return crypto.Sign(hash.Bytes(), k.privKey)
}

func (k *EspressoKeyManager) RegisterService() error {
	teeType := k.TeeType()
	switch teeType {
	case SGX:
		return k.Register(k.getAttestationQuote)
	case NITRO:
		return k.Register(k.getNitroAttestation)
	case TESTS:
		return k.Register(k.noOpSignerFunc)
	default:
		return fmt.Errorf("unsupported tee Type: %d", teeType)
	}
}

func (k *EspressoKeyManager) getAttestationQuoteForTests(userData []byte) ([]byte, error) {
	return []byte{}, nil
}

// getAttestationQuote is a method that retrieves the attestation quote for the user data.
// This function generates the attestation quote for the user data.
// The user data is hashed using keccak256 and then 32 bytes of padding is added to the hash.
// The hash is then written to a file specified in the config. (For SGX: /dev/attestation/user_report_data)
// The quote is then read from the file specified in the config. (For SGX: /dev/attestation/quote)
func (k *EspressoKeyManager) getAttestationQuote(userData []byte) ([]byte, error) {

	if (k.userDataAttestationFile == "") || (k.quoteFile == "") {
		return []byte{}, nil
	}
	// keccak256 hash of userData
	userDataHash := crypto.Keccak256(userData)

	// Add 32 bytes of padding to the user data hash
	// because keccak256 hash is 32 bytes and sgx requires 64 bytes of user data
	for i := 0; i < 32; i += 1 {
		userDataHash = append(userDataHash, 0)
	}

	// Write the message to "/dev/attestation/user_report_data" in SGX
	err := os.WriteFile(k.userDataAttestationFile, userDataHash, 0600)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to create user report data file: %w", err)
	}

	// Read the quote from "/dev/attestation/quote" in SGX
	attestationQuote, err := os.ReadFile(k.quoteFile)
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

	attestation, err := nitrite.Verify(res.Attestation.Document, nitrite.VerifyOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to verify attestation")
	}

	attestationBytes, err := json.Marshal(attestation)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal attestation")
	}
	return attestationBytes, nil
}

// No-Op Signauture
// This is a function designed to replace a signing function for functionality that depends on operating in a TEE

func (k *EspressoKeyManager) noOpSignerFunc(payload []byte) ([]byte, error) {
	return payload, nil
}

func SetupNitroVerifier(teeVerifier *espressogen.IEspressoTEEVerifier, l1Client *ethclient.Client) (espressotee.EspressoNitroTEEVerifierInterface, error) {
	// Setup nitro contract interface
	nitroAddr, err := teeVerifier.EspressoNitroTEEVerifier(&bind.CallOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to get nitro tee verifier address from caller: %w", err)
	}
	log.Info("successfully retrieved nitro contract verifier address", "address", nitroAddr)

	nitroVerifierBindings, err := espressogen.NewIEspressoNitroTEEVerifier(
		nitroAddr,
		l1Client)
	if err != nil {
		return nil, err
	}
	nitroVerifier := espressotee.NewEspressoNitroTEEVerifier(nitroVerifierBindings, l1Client, nitroAddr)
	return nitroVerifier, nil
}
