package integrityattestation

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"os"
	"path"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/distributed-lab/enclave-extras/nsm"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	// Attestation document with the KMS Key ID
	// in UserData attestation doc field.
	kmsKeyIDFile = "kms_key_id.coses1"
	// Attestation document with the encrypted
	// private key in UserData attestation doc field.
	privateKeyFile = "private_key.coses1"
	// Attestation document with the public key in
	// UserData and PublicKey attestation doc fields.
	publicKeyFile = "public_key.coses1"
	// Attestation document with the Ethereum
	// address in UserData attestation doc field.
	addressFile = "address.coses1"
)

func GetAttestedKMSKeyID(cfg aws.Config, attestationsPath string) (string, error) {
	kmsKeyIDPath := path.Join(attestationsPath, kmsKeyIDFile)

	kmsKeyIDAttestationDocRaw, err := os.ReadFile(kmsKeyIDPath)
	// if attestation document exist just read KMS Key ID
	if err == nil {
		kmsKeyIDAttestationDoc, err := parseAndVerifyAttestationDocument(kmsKeyIDAttestationDocRaw)
		if err != nil {
			return "", fmt.Errorf("failed to parse %s: %w", kmsKeyIDPath, err)
		}

		return string(kmsKeyIDAttestationDoc.UserData), nil
	}

	// if attestation document exists, but we can't open file
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to read %s, check file permissions. err: %w", kmsKeyIDPath, err)
	}

	rootArn, principalArn, err := GetArns(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to get root and principal arns for kms key policy: %w", err)
	}

	kmsEnclaveClient, err := GetKMSEnclaveClient(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to get kms enclave client: %w", err)
	}

	_, pcr0Actual, err := nsm.DescribePCR(0)
	if err != nil {
		return "", fmt.Errorf("failed to get PCR0: %w", err)
	}

	kmsKeyPolicy := DefaultPolicies(rootArn, principalArn, map[int][]byte{0: pcr0Actual})
	createKeyOutput, err := kmsEnclaveClient.CreateKey(context.Background(), &kms.CreateKeyInput{
		// DANGER: The key may become unmanageable
		BypassPolicyLockoutSafetyCheck: true,
		Description:                    aws.String("Nitro Enclave Key"),
		Policy:                         aws.String(kmsKeyPolicy),
	})
	if err != nil {
		return "", fmt.Errorf("failed to create KMS key: %w", err)
	}

	kmsKeyID := deref(createKeyOutput.KeyMetadata.KeyId)

	// Save KMS Key
	kmsKeyIDAttestationDocRaw, err = nsm.GetAttestationDoc([]byte(kmsKeyID), nil, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get attestation document for %s: %w", kmsKeyIDPath, err)
	}
	if err = os.WriteFile(kmsKeyIDPath, kmsKeyIDAttestationDocRaw, 0600); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", kmsKeyIDPath, err)
	}

	return kmsKeyID, nil
}

func GetAttestedPrivateKey(cfg aws.Config, kmsKeyID string, attestationsPath string) (*ecdsa.PrivateKey, error) {
	kmsEnclaveClient, err := GetKMSEnclaveClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get kms enclave client: %w", err)
	}

	privateKeyPath := path.Join(attestationsPath, privateKeyFile)
	privateKeyAttestationDocRaw, err := os.ReadFile(privateKeyPath)
	// if attestation document exist just read and decrypt private key
	if err == nil {
		privateKeyAttestationDoc, err := parseAndVerifyAttestationDocument(privateKeyAttestationDocRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", privateKeyPath, err)
		}

		decryptResp, err := kmsEnclaveClient.Decrypt(context.Background(), &kms.DecryptInput{
			KeyId:          aws.String(kmsKeyID),
			CiphertextBlob: privateKeyAttestationDoc.UserData,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt private key: %w", err)
		}

		privateKey, err := parsePKCS8ECPrivateKey(decryptResp.Plaintext)
		if err != nil {
			return nil, fmt.Errorf("failed to parse secp256k1: %w", err)
		}

		return privateKey, nil
	}

	// if attestation document exists, but we can't open file
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read %s, check file permissions. err: %w", privateKeyPath, err)
	}

	// Create private key
	generateDataKeyPairResp, err := kmsEnclaveClient.GenerateDataKeyPair(context.Background(), &kms.GenerateDataKeyPairInput{
		KeyId:       aws.String(kmsKeyID),
		KeyPairSpec: kmstypes.DataKeyPairSpecEccSecgP256k1,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate secp256k1 in KMS: %w", err)
	}

	privateKey, err := parsePKCS8ECPrivateKey(generateDataKeyPairResp.PrivateKeyPlaintext)
	if err != nil {
		return nil, fmt.Errorf("failed to parse secp256k1: %w", err)
	}

	// Save private key
	privateKeyAttestationDocRaw, err = nsm.GetAttestationDoc(generateDataKeyPairResp.PrivateKeyCiphertextBlob, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get attestation doc for %s: %w", privateKeyPath, err)
	}
	if err = os.WriteFile(privateKeyPath, privateKeyAttestationDocRaw, 0600); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", privateKeyPath, err)
	}

	return privateKey, nil
}

func GetAttestedPublicKey(privateKey *ecdsa.PrivateKey, attestationsPath string) (*ecdsa.PublicKey, error) {
	publicKeyPath := path.Join(attestationsPath, publicKeyFile)

	// if attestation document exist just read public key
	_, err := os.ReadFile(publicKeyPath)
	if err == nil {
		return &privateKey.PublicKey, nil
	}

	// if attestation document exists, but we can't open file
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read %s, check file permissions. err: %w", publicKeyPath, err)
	}

	publicKey := &privateKey.PublicKey

	// Save public key
	publicKeyAttestationDocRaw, err := nsm.GetAttestationDoc(crypto.FromECDSAPub(publicKey), nil, crypto.FromECDSAPub(publicKey))
	if err != nil {
		return nil, fmt.Errorf("failed to get attestation doc for %s: %w", publicKeyPath, err)
	}
	if err = os.WriteFile(publicKeyPath, publicKeyAttestationDocRaw, 0600); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", publicKeyPath, err)
	}

	return publicKey, nil
}

func GetAttestedAddress(publicKey *ecdsa.PublicKey, attestationsPath string) (common.Address, error) {
	address := crypto.PubkeyToAddress(*publicKey)

	addressPath := path.Join(attestationsPath, addressFile)

	// if attestation document exist just read address
	_, err := os.ReadFile(addressPath)
	if err == nil {
		return address, nil
	}

	// if attestation document exists, but we can't open file
	if !os.IsNotExist(err) {
		return address, fmt.Errorf("failed to read %s, check file permissions. err: %w", addressPath, err)
	}

	// Save address
	addressAttestationDocRaw, err := nsm.GetAttestationDoc(address[:], nil, nil)
	if err != nil {
		return address, fmt.Errorf("failed to get attestation doc for %s: %w", addressPath, err)
	}
	if err = os.WriteFile(addressPath, addressAttestationDocRaw, 0600); err != nil {
		return address, fmt.Errorf("failed to write %s: %w", addressPath, err)
	}

	return address, nil
}
