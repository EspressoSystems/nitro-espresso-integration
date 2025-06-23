package espressotee

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

type TEE uint8

const (
	SGX   TEE = 0 // SGX
	NITRO TEE = 1 // AWS Nitro
)

func (t TEE) FromString(s string) (TEE, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "SGX":
		return SGX, nil
	case "NITRO":
		return NITRO, nil
	default:
		return 0, fmt.Errorf("invalid TEE type: %q", s)
	}
}

type EspressoRegisterSignerConfig struct {
	MaxTxnWaitTime                time.Duration `koanf:"max-txn-wait-time"`
	RetryBaseFeeDelay             time.Duration `koanf:"retry-base-fee-delay"`
	RetryReadContractDelay        time.Duration `koanf:"retry-read-contract-delay"`
	MaxRetries                    uint8         `koanf:"max-retries"`
	GasLimitBufferIncreasePercent uint64        `koanf:"gas-limit-buffer-increase-percent"`
	MaxBaseFee                    uint64        `koanf:"max-base-fee"`
}

var DefaultEspressoRegisterSignerConfig = EspressoRegisterSignerConfig{
	MaxTxnWaitTime:                3 * time.Minute,
	RetryBaseFeeDelay:             1 * time.Minute,
	RetryReadContractDelay:        5 * time.Second,
	MaxRetries:                    5,
	GasLimitBufferIncreasePercent: 20,
	MaxBaseFee:                    70000000,
}

type EspressoRegisterSignerOpts struct {
	MaxTxnWaitTime                time.Duration
	RetryBaseFeeDelay             time.Duration
	RetryReadContractDelay        time.Duration
	MaxRetries                    int
	GasLimitBufferIncreasePercent uint64
	MaxBaseFee                    uint64
}

func AddEspressoRegisterSignerConfigOptions(prefix string, f *pflag.FlagSet) {
	f.Duration(prefix+".max-txn-wait-time", DefaultEspressoRegisterSignerConfig.MaxTxnWaitTime, "max transaction wait time when calling espresso tee verifier contracts")
	f.Duration(prefix+".retry-send-delay", DefaultEspressoRegisterSignerConfig.RetryBaseFeeDelay, "delay in calls to check the base fee")
	f.Duration(prefix+".retry-send-delay", DefaultEspressoRegisterSignerConfig.RetryReadContractDelay, "delay in calls to read from contract for verification")
	f.Int(prefix+".max-retries", int(DefaultEspressoRegisterSignerConfig.MaxRetries), "how many times to check if we have data in our espresso tee contracts")
	f.Uint64(prefix+".gas-limit-buffer-increase-percent", DefaultEspressoRegisterSignerConfig.GasLimitBufferIncreasePercent, "buffer increase to gas limit in espresso tee contracts")
	f.Uint64(prefix+".max-base-fee", DefaultEspressoRegisterSignerConfig.MaxBaseFee, "max base fee to use when calling espresso tee contracts")
}
