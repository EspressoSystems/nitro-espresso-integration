package integrityattestation

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/distributed-lab/enclave-extras/attestation"
	"github.com/distributed-lab/enclave-extras/attestedkms"
	"github.com/distributed-lab/enclave-extras/nsm"

	"github.com/offchainlabs/nitro/util/signature"
)

const (
	AwsIamServiceID = "iam"
	AwsStsServiceID = "sts"
)

func EnsureArnIsIam(v string) (string, error) {
	resourceArn, err := arn.Parse(v)
	if err != nil {
		return "", fmt.Errorf("failed to parse resource ARN: %w", err)
	}

	// If ARN service already IAM just return it
	if resourceArn.Service == AwsIamServiceID {
		return v, nil
	}

	if resourceArn.Service != AwsStsServiceID || !strings.HasPrefix(resourceArn.Resource, "assumed-role/") {
		return "", fmt.Errorf("unsuported conversion, can convert only STS assumed-role in IAM role")
	}

	resourceArn.Service = AwsIamServiceID
	// Should never be out of range, because of AWS guarantee that role can't be empty string
	resourceArn.Resource = "role/" + strings.Split(resourceArn.Resource, "/")[1]

	return resourceArn.String(), nil
}

func ToRootArn(v string) (string, error) {
	resourceArn, err := arn.Parse(v)
	if err != nil {
		return "", fmt.Errorf("failed to parse resource ARN: %w", err)
	}

	resourceArn.Service = AwsIamServiceID
	resourceArn.Resource = "root"

	return resourceArn.String(), nil
}

func GetArns(cfg aws.Config) (rootArn string, principalArn string, err error) {
	stsClient := sts.NewFromConfig(cfg)
	callerIdentityResponse, err := stsClient.GetCallerIdentity(context.Background(), &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", "", fmt.Errorf("failed to get caller identity: %w", err)
	}

	principalArn, err = EnsureArnIsIam(deref(callerIdentityResponse.Arn))
	if err != nil {
		return "", "", fmt.Errorf("failed to cast arn: %w", err)
	}

	rootArn, err = ToRootArn(principalArn)
	if err != nil {
		return "", "", fmt.Errorf("failed to make root arn: %w", err)
	}

	return rootArn, principalArn, nil
}

func GetKMSEnclaveClient(cfg aws.Config) (*attestedkms.KMSEnclaveClient, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA private key: %w", err)
	}

	derEncodedPublicKey, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key PKIX: %w", err)
	}

	attestationDoc, err := nsm.GetAttestationDoc(nil, nil, derEncodedPublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get attestation document: %w", err)
	}

	return attestedkms.NewFromConfig(cfg, attestationDoc, privateKey), nil
}

func ReadEnclavePrivateKey(attestationsPath string) (*ecdsa.PublicKey, signature.DataSignerFunc, error) {
	if err := os.MkdirAll(attestationsPath, os.ModePerm); err != nil {
		return nil, nil, fmt.Errorf("failed to create attestations path directory %s with error: %w", attestationsPath, err)
	}

	awsConfig, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	kmsKeyID, err := GetAttestedKMSKeyID(awsConfig, attestationsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get attested KMS Key ID: %w", err)
	}

	privateKey, err := GetAttestedPrivateKey(awsConfig, kmsKeyID, attestationsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get attested private key: %w", err)
	}

	publicKey, err := GetAttestedPublicKey(privateKey, attestationsPath)
	if err != nil || publicKey == nil {
		return nil, nil, fmt.Errorf("failed to get attested public key: %w", err)
	}

	return publicKey, signature.DataSignerFromPrivateKey(privateKey), nil
}

// Safely pointer dereference
func deref[T any](p *T) T {
	if p != nil {
		return *p
	}
	// Declares a variable of type T, initialized to its zero value
	var zero T
	return zero
}

func parsePKCS8ECPrivateKey(pcks8PrivateKey []byte) (*ecdsa.PrivateKey, error) {
	privateKeyAny, err := attestedkms.ParsePKCS8PrivateKey(pcks8PrivateKey)
	if err != nil {
		return nil, err
	}

	privateKey, ok := privateKeyAny.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("invalid EC private key")
	}

	return privateKey, nil
}

func parseAndVerifyAttestationDocument(rawDocument []byte) (*attestation.NSMAttestationDoc, error) {
	attestationDoc, err := attestation.ParseNSMAttestationDoc(rawDocument)
	if err != nil {
		return nil, fmt.Errorf("failed to parse attestation document: %w", err)
	}

	if err = attestationDoc.Verify(); err != nil {
		return nil, fmt.Errorf("attestation document have invalid signature: %w", err)
	}

	_, pcr0Actual, err := nsm.DescribePCR(0)
	if err != nil {
		return nil, fmt.Errorf("failed to get PCR0: %w", err)
	}

	if pcr0Stored, ok := attestationDoc.PCRs[0]; !ok || !bytes.Equal(pcr0Stored, pcr0Actual) {
		return nil, fmt.Errorf("PCR0 from attestation document mismatch with actual PCR0 value")
	}

	return attestationDoc, nil
}
