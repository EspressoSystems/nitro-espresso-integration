package espressotee

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/distributed-lab/enclave-extras/attestation"
	"github.com/distributed-lab/enclave-extras/attestation/kmshelpers"
	"github.com/distributed-lab/enclave-extras/nsm"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/offchainlabs/nitro/cmd/genericconf"
)

type KMSAttestationConfig struct {
	attestationDoc []byte
	pk             *rsa.PrivateKey
}

const (
	awsConfigValidatorProfile = "validator"
	enclaveWalletSuffix       = ".enclave"

	// Attestation document with the validator's encrypted
	// private key in UserData attestation doc field.
	privateKeyFile = "private_key.coses1"
	// Attestation document with the KMS KeyID
	// in UserData attestation doc field.
	kmsKeyIDFile = "kms_key_id.coses1"
	// Attestation document with the validator's address
	// in UserData attestation doc field.
	addressFile = "address.coses1"
)

func OpenEnclaveValidatorWallet(description string, walletConfig *genericconf.WalletConfig, chainId *big.Int) (*bind.TransactOpts, error) {
	awsConfig, err := config.LoadDefaultConfig(context.Background(), config.WithSharedConfigProfile(awsConfigValidatorProfile))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS validator config: %w", err)
	}

	stsClient := sts.NewFromConfig(awsConfig)
	kmsEnclaveClient := kmshelpers.NewFromConfig(awsConfig)
	kmsAttestationConfig, err := newKMSAttestationConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get KMS attestation config: %w", err)
	}

	_, pcr0Actual, err := nsm.DescribePCR(0)
	if err != nil {
		return nil, fmt.Errorf("failed to get PCR0: %w", err)
	}

	enclaveWalletPath := walletConfig.Pathname + enclaveWalletSuffix
	if err := os.MkdirAll(enclaveWalletPath, 0o700); err != nil {
		return nil, err
	}
	kmsKeyIDPath := path.Join(enclaveWalletPath, kmsKeyIDFile)
	privateKeyPath := path.Join(enclaveWalletPath, privateKeyFile)
	addressPath := path.Join(enclaveWalletPath, addressFile)

	// Read or create KMS Key
	var kmsKeyID string
	kmsKeyIDAttestationDocRaw, err := os.ReadFile(kmsKeyIDPath)
	switch {
	case err == nil:
		kmsKeyIDAttestationDoc, err := attestation.ParseNSMAttestationDoc(kmsKeyIDAttestationDocRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", kmsKeyIDPath, err)
		}
		if err = kmsKeyIDAttestationDoc.Verify(); err != nil {
			return nil, fmt.Errorf("%s have invalid signature: %w", kmsKeyIDPath, err)
		}
		if pcr0Stored, ok := kmsKeyIDAttestationDoc.PCRs[0]; !ok || !bytes.Equal(pcr0Stored, pcr0Actual) {
			return nil, fmt.Errorf("PCR0 from %s mismatch with actual PCR0 value", kmsKeyIDPath)
		}
		kmsKeyID = string(kmsKeyIDAttestationDoc.UserData)
	case os.IsNotExist(err):
		// Create KMS Key
		getCallerIdentityOutput, err := stsClient.GetCallerIdentity(context.Background(), &sts.GetCallerIdentityInput{})
		if err != nil {
			return nil, fmt.Errorf("failed to get caller identity: %w", err)
		}

		rootARN, err := arn.Parse(safeStringDeref(getCallerIdentityOutput.Arn))
		if err != nil {
			return nil, fmt.Errorf("failed to parse caller ARN: %w", err)
		}
		rootARN.Resource = "root"

		kmsKeyPolicy := defaultEnclaveKMSKeyPolicies(rootARN.String(), safeStringDeref(getCallerIdentityOutput.Arn), map[string]string{
			nsm.PCRxCondition(0): hex.EncodeToString(pcr0Actual),
		})

		createKeyOutput, err := kmsEnclaveClient.CreateKey(context.Background(), &kms.CreateKeyInput{
			// DANGER: The key may become unmanageable
			BypassPolicyLockoutSafetyCheck: true,
			Description:                    aws.String("Nitro Enclave Key"),
			Policy:                         aws.String(kmsKeyPolicy),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create KMS key: %w", err)
		}
		kmsKeyID = safeStringDeref(createKeyOutput.KeyMetadata.KeyId)

		// Save KMS Key
		kmsKeyIDAttestationDocRaw, err = nsm.GetAttestationDoc([]byte(kmsKeyID), nil, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get attestation document for %s: %w", kmsKeyIDPath, err)
		}
		if err = os.WriteFile(kmsKeyIDPath, kmsKeyIDAttestationDocRaw, 0600); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", kmsKeyIDPath, err)
		}
	default:
		return nil, fmt.Errorf("failed to read %s, check file permissions. err: %w", kmsKeyIDPath, err)
	}

	// Read or create Secp256k1 private key
	var privateKey *ecdsa.PrivateKey
	privateKeyAttestationDocRaw, err := os.ReadFile(privateKeyPath)
	switch {
	case err == nil:
		privateKeyAttestationDoc, err := attestation.ParseNSMAttestationDoc(privateKeyAttestationDocRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", privateKeyPath, err)
		}
		if err = privateKeyAttestationDoc.Verify(); err != nil {
			return nil, fmt.Errorf("%s have invalid signature: %w", privateKeyPath, err)
		}
		if pcr0Stored, ok := privateKeyAttestationDoc.PCRs[0]; !ok || !bytes.Equal(pcr0Stored, pcr0Actual) {
			return nil, fmt.Errorf("PCR0 from %s mismatch with actual PCR0 value", privateKeyPath)
		}
		decryptResp, err := kmsEnclaveClient.Decrypt(context.Background(), &kms.DecryptInput{
			KeyId:          aws.String(kmsKeyID),
			CiphertextBlob: privateKeyAttestationDoc.UserData,
			Recipient: &kmstypes.RecipientInfo{
				AttestationDocument:    kmsAttestationConfig.attestationDoc,
				KeyEncryptionAlgorithm: kmstypes.KeyEncryptionMechanismRsaesOaepSha256,
			},
		}, kmsAttestationConfig.pk)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt private key: %w", err)
		}
		privateKey, err = parsePKCS8ECPrivateKey(decryptResp.Plaintext)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
	case os.IsNotExist(err):
		// Create private key
		generateDataKeyPairResp, err := kmsEnclaveClient.GenerateDataKeyPair(context.Background(), &kms.GenerateDataKeyPairInput{
			KeyId:       aws.String(kmsKeyID),
			KeyPairSpec: kmstypes.DataKeyPairSpecEccSecgP256k1,
			Recipient: &kmstypes.RecipientInfo{
				AttestationDocument:    kmsAttestationConfig.attestationDoc,
				KeyEncryptionAlgorithm: kmstypes.KeyEncryptionMechanismRsaesOaepSha256,
			},
		}, kmsAttestationConfig.pk)
		if err != nil {
			return nil, fmt.Errorf("failed to generate secp256k1 in KMS: %w", err)
		}
		privateKey, err = parsePKCS8ECPrivateKey(generateDataKeyPairResp.PrivateKeyPlaintext)
		if err != nil {
			return nil, fmt.Errorf("failed to parse")
		}

		// Save private key
		privateKeyAttestationDocRaw, err = nsm.GetAttestationDoc(generateDataKeyPairResp.PrivateKeyCiphertextBlob, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get attestation doc for %s: %w", privateKeyPath, err)
		}
		if err = os.WriteFile(privateKeyPath, privateKeyAttestationDocRaw, 0600); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", privateKeyPath, err)
		}
	default:
		return nil, fmt.Errorf("failed to read %s, check file permissions. err: %w", privateKeyPath, err)
	}

	// Save address if file not exist
	addressAttestationDocRaw, err := os.ReadFile(addressPath)
	switch {
	case err == nil:
		addressAttestationDoc, err := attestation.ParseNSMAttestationDoc(addressAttestationDocRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", addressPath, err)
		}
		if err = addressAttestationDoc.Verify(); err != nil {
			return nil, fmt.Errorf("%s have invalid signature: %w", addressPath, err)
		}
		if pcr0Stored, ok := addressAttestationDoc.PCRs[0]; !ok || !bytes.Equal(pcr0Stored, pcr0Actual) {
			return nil, fmt.Errorf("PCR0 from %s mismatch with actual PCR0 value", addressPath)
		}
	case os.IsNotExist(err):
		// Save address
		address := crypto.PubkeyToAddress(privateKey.PublicKey)
		addressAttestationDocRaw, err = nsm.GetAttestationDoc(address[:], nil, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get attestation document for %s: %w", addressPath, err)
		}
		if err = os.WriteFile(addressPath, addressAttestationDocRaw, 0600); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", addressPath, err)
		}
	default:
		return nil, fmt.Errorf("failed to read %s, check file permissions. err: %w", addressPath, err)
	}

	if walletConfig.OnlyCreateKey {
		return nil, nil
	}

	txOpts, err := bind.NewKeyedTransactorWithChainID(privateKey, chainId)
	if err != nil {
		return nil, fmt.Errorf("failed to create keyed transactor: %w", err)
	}

	return txOpts, nil
}

// Prepare config for use with KMSEnclaveClient
func newKMSAttestationConfig() (*KMSAttestationConfig, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA private key: %w", err)
	}
	derEncodedPublicKey, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key PKIX: %w", err)
	}
	kmsAttestationDocRaw, err := nsm.GetAttestationDoc(nil, nil, derEncodedPublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get attestation doc with public key: %w", err)
	}

	return &KMSAttestationConfig{
		attestationDoc: kmsAttestationDocRaw,
		pk:             privateKey,
	}, nil
}

func parsePKCS8ECPrivateKey(pcks8PrivateKey []byte) (*ecdsa.PrivateKey, error) {
	privateKeyAny, err := kmshelpers.ParsePKCS8PrivateKey(pcks8PrivateKey)
	if err != nil {
		return nil, err
	}

	privateKey, ok := privateKeyAny.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("invalid EC private key")
	}

	return privateKey, nil
}

func defaultEnclaveKMSKeyPolicies(rootARN, principalARN string, pcrs map[string]string) string {
	defaultPolicy := map[string]interface{}{
		"Version": "2012-10-17",
		"Id":      "key-default-1",
		"Statement": []map[string]interface{}{
			{
				"Sid":    "Allow access for Key Administrators",
				"Effect": "Allow",
				"Principal": map[string]interface{}{
					"AWS": rootARN,
				},
				"Action": []string{
					"kms:CancelKeyDeletion",
					"kms:DescribeKey",
					"kms:DisableKey",
					"kms:EnableKey",
					"kms:GetKeyPolicy",
					"kms:ScheduleKeyDeletion",
				},
				"Resource": "*",
			},
			{
				"Sid":    "Enable enclave",
				"Effect": "Allow",
				"Principal": map[string]interface{}{
					"AWS": principalARN,
				},
				"Action": []string{
					"kms:Decrypt",
					"kms:GenerateRandom",
					"kms:GenerateDataKey",
					"kms:GenerateDataKeyPair",
				},
				"Resource": "*",
				"Condition": map[string]interface{}{
					"StringEqualsIgnoreCase": pcrs,
				},
			},
		},
	}

	// should never panic
	policy, _ := json.Marshal(defaultPolicy)
	return string(policy)
}

func safeStringDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
