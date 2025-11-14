package espresso_tee_utils

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Return PCRx condition to be used when creating a KMS key
func PcrXCondition(pcrIndex int) string {
	return fmt.Sprintf("kms:RecipientAttestation:PCR%d", pcrIndex)
}

func DefaultPolicies(rootARN, principalARN string, pcrs map[int][]byte) string {
	pcrConditions := make(map[string]string, len(pcrs))
	for k, v := range pcrs {
		pcrConditions[PcrXCondition(k)] = hex.EncodeToString(v)
	}

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
					"StringEqualsIgnoreCase": pcrConditions,
				},
			},
		},
	}

	// should never panic
	policy, _ := json.Marshal(defaultPolicy)
	return string(policy)
}
