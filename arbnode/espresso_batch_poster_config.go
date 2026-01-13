package arbnode

import (
	"time"

	"github.com/spf13/pflag"

	"github.com/offchainlabs/nitro/espressotee"
)

type EspressoBatchPosterConfig struct {
	EspressoTeeType                  string                                    `koanf:"espresso-tee-type"`
	EspressoRegisterServiceConfig    espressotee.EspressoRegisterServiceConfig `koanf:"espresso-register-service-config"`
	HotShotUrl                       string                                    `koanf:"hotshot-url"`
	EspressoTxnsPollingInterval      time.Duration                             `koanf:"espresso-txns-polling-interval"`
	EspressoTxnsSendingInterval      time.Duration                             `koanf:"espresso-txns-sending-interval"`
	EspressoTxnsResubmissionInterval time.Duration                             `koanf:"espresso-txns-resubmission-interval"`
	ResubmitEspressoTxDeadline       time.Duration                             `koanf:"resubmit-espresso-tx-deadline"`
	EspressoTxSizeLimit              int64                                     `koanf:"espresso-tx-size-limit"`
	UserDataAttestationFile          string                                    `koanf:"user-data-attestation-file"`
	QuoteFile                        string                                    `koanf:"quote-file"`
	AttestationServiceURL            string                                    `koanf:"attestation-service-url"`

	// Fetch messages from HotShot block
	// HotShotBlock             uint64 `koanf:"hotshot-block"`
	EspressoEventPollingStep uint64 `koanf:"espresso-event-polling-step"`
	HotShotFirstPostingBlock uint64 `koanf:"hotshot-first-posting-block"`
	// Please make sure that these addresses are already valid at the `AddressMonitorStartL1`
	AddressMonitorStartL1 uint64   `koanf:"address-monitor-start-l1"`
	InitBatcherAddresses  []string `koanf:"init-batcher-addresses"`
	AddressMonitorStep    uint64   `koanf:"address-monitor-step"`

	AddressValidRanges []AddressValidRangeConfig `koanf:"address-valid-ranges"`
}

func EspressoBatchPosterConfigAddOptions(prefix string, f *pflag.FlagSet) {
	f.String(prefix+".espresso-tee-type", DefaultEspressoBatchPosterConfig.EspressoTeeType, "the Trusted Execution Environment (TEE) that Batch poster is running in")
	f.String(prefix+".hotshot-url", DefaultEspressoBatchPosterConfig.HotShotUrl, "specifies the hotshot url if we are batching in espresso mode")
	f.Uint64(prefix+".hotshot-first-posting-block", DefaultEspressoBatchPosterConfig.HotShotFirstPostingBlock, "specifies the l1 block number when this rollup started posting to hotshot")
	f.Uint64(prefix+".espresso-event-polling-step", DefaultEspressoBatchPosterConfig.EspressoEventPollingStep, "specifies the number of blocks at a time to query when searching for logs emitted by batch posting.")
	f.Duration(prefix+".espresso-txns-polling-interval", DefaultEspressoBatchPosterConfig.EspressoTxnsPollingInterval, "interval between polling for transactions to be included in the block")
	f.Duration(prefix+".espresso-txns-sending-interval", DefaultEspressoBatchPosterConfig.EspressoTxnsSendingInterval, "interval between sending transactions to Espresso Network")
	f.Duration(prefix+".espresso-txns-resubmission-interval", DefaultEspressoBatchPosterConfig.EspressoTxnsResubmissionInterval, "interval between checking if the node should resubmitting transactions to Espresso Network")
	f.Duration(prefix+".resubmit-espresso-tx-deadline", DefaultEspressoBatchPosterConfig.ResubmitEspressoTxDeadline, "time threshold after which a transaction will be automatically resubmitted if no response is received")
	f.String(prefix+".user-data-attestation-file", DefaultEspressoBatchPosterConfig.UserDataAttestationFile, "path to SGX user data attestation file")
	f.String(prefix+".quote-file", DefaultEspressoBatchPosterConfig.QuoteFile, "path to SGX quote file")
	f.String(prefix+".attestation-service-url", DefaultEspressoBatchPosterConfig.AttestationServiceURL, "URL of the attestation service to use for obtaining zk proof over  attestation")
	f.Int64(prefix+".espresso-tx-size-limit", DefaultEspressoBatchPosterConfig.EspressoTxSizeLimit, "specifies the maximum size of a transaction to be sent to the Espresso Network")
	f.StringSlice(prefix+".init-batcher-addresses", DefaultEspressoBatchPosterConfig.InitBatcherAddresses, "specifies the init batcher addresses")
	f.Uint64(prefix+".address-monitor-step", DefaultEspressoBatchPosterConfig.AddressMonitorStep, "specifies the number of blocks at a time to query when searching for logs emitted for updating valid batcher addresses.")
	f.Uint64(prefix+".address-monitor-start-l1", DefaultEspressoBatchPosterConfig.AddressMonitorStartL1, "specifies the l1 block number when this rollup started posting to monitor addresses")
}

var DefaultEspressoBatchPosterConfig = EspressoBatchPosterConfig{
	// Hotshot currently produces blocks at average of 2 seconds
	// We set it to 1 second to get updates more often than blocks are produced
	EspressoTxnsPollingInterval: time.Second,
	// We should send to espresso at a speed faster than the speed nitro is producing messages
	EspressoTxnsSendingInterval:      125 * time.Millisecond,
	EspressoTxnsResubmissionInterval: 2 * time.Second,
	ResubmitEspressoTxDeadline:       10 * time.Minute,
	HotShotUrl:                       "",
	EspressoTeeType:                  "NITRO",
	EspressoRegisterServiceConfig:    espressotee.DefaultEspressoRegisterServiceConfig,
	// EspressoTxSizeLimit is 1 MB, to have some buffer we set it to 900 KB
	EspressoTxSizeLimit:      900 * 1024,
	UserDataAttestationFile:  "",
	QuoteFile:                "",
	AttestationServiceURL:    "",
	HotShotFirstPostingBlock: 1,
	InitBatcherAddresses:     []string{},
	EspressoEventPollingStep: 100,
	AddressMonitorStep:       100,
	AddressMonitorStartL1:    1,
	AddressValidRanges:       []AddressValidRangeConfig{},
}

var TestEspressoBatchPosterConfig = EspressoBatchPosterConfig{
	EspressoTxnsPollingInterval:      time.Second,
	EspressoTxnsSendingInterval:      time.Second,
	EspressoTxnsResubmissionInterval: 2 * time.Second,
	ResubmitEspressoTxDeadline:       10 * time.Second,
	HotShotUrl:                       "",
	EspressoTeeType:                  "TESTS",
	EspressoRegisterServiceConfig:    espressotee.DefaultEspressoRegisterServiceConfig,
	EspressoTxSizeLimit:              200 * 1024,

	HotShotFirstPostingBlock: 1,
	InitBatcherAddresses:     []string{},
	EspressoEventPollingStep: 100,
	AddressMonitorStartL1:    1,
	AddressMonitorStep:       100,
	AddressValidRanges:       []AddressValidRangeConfig{},
}
