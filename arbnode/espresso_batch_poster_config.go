package arbnode

import (
	"time"

	"github.com/spf13/pflag"

	"github.com/offchainlabs/nitro/espressotee"
)

type EspressoBatchPosterConfig struct {
	TeeType                    string                                    `koanf:"tee-type"`
	RegisterServiceConfig      espressotee.EspressoRegisterServiceConfig `koanf:"register-service-config"`
	HotShotUrl                 string                                    `koanf:"hotshot-url"`
	TxnsSendingInterval        time.Duration                             `koanf:"txns-sending-interval"`
	TxnsResubmissionInterval   time.Duration                             `koanf:"txns-resubmission-interval"`
	ResubmitEspressoTxDeadline time.Duration                             `koanf:"resubmit-espresso-tx-deadline"`
	TxSizeLimit                int64                                     `koanf:"tx-size-limit"`
	UserDataAttestationFile    string                                    `koanf:"user-data-attestation-file"`
	QuoteFile                  string                                    `koanf:"quote-file"`
	AttestationServiceURL      string                                    `koanf:"attestation-service-url"`

	EventPollingStep         uint64 `koanf:"event-polling-step"`
	HotShotFirstPostingBlock uint64 `koanf:"hotshot-first-posting-block"`
	// Please make sure that these addresses are already valid at the `AddressMonitorStartL1`
	InitBatcherAddresses []string `koanf:"init-batcher-addresses"`

	AddressValidRanges []AddressValidRangeConfig `koanf:"address-valid-ranges"`
}

func EspressoBatchPosterConfigAddOptions(prefix string, f *pflag.FlagSet) {
	f.String(prefix+".tee-type", DefaultEspressoBatchPosterConfig.TeeType, "the Trusted Execution Environment (TEE) that Batch poster is running in")
	f.String(prefix+".hotshot-url", DefaultEspressoBatchPosterConfig.HotShotUrl, "specifies the hotshot url if we are batching in espresso mode")
	f.Uint64(prefix+".hotshot-first-posting-block", DefaultEspressoBatchPosterConfig.HotShotFirstPostingBlock, "specifies the l1 block number when this rollup started posting to hotshot")
	f.Uint64(prefix+".event-polling-step", DefaultEspressoBatchPosterConfig.EventPollingStep, "specifies the number of blocks at a time to query when searching for logs emitted by batch posting.")
	f.Duration(prefix+".txns-sending-interval", DefaultEspressoBatchPosterConfig.TxnsSendingInterval, "interval between sending transactions to Espresso Network")
	f.Duration(prefix+".txns-resubmission-interval", DefaultEspressoBatchPosterConfig.TxnsResubmissionInterval, "interval between checking if the node should resubmitting transactions to Espresso Network")
	f.Duration(prefix+".resubmit-espresso-tx-deadline", DefaultEspressoBatchPosterConfig.ResubmitEspressoTxDeadline, "time threshold after which a transaction will be automatically resubmitted if no response is received")
	f.String(prefix+".user-data-attestation-file", DefaultEspressoBatchPosterConfig.UserDataAttestationFile, "path to SGX user data attestation file")
	f.String(prefix+".quote-file", DefaultEspressoBatchPosterConfig.QuoteFile, "path to SGX quote file")
	f.String(prefix+".attestation-service-url", DefaultEspressoBatchPosterConfig.AttestationServiceURL, "URL of the attestation service to use for obtaining zk proof over  attestation")
	f.Int64(prefix+".tx-size-limit", DefaultEspressoBatchPosterConfig.TxSizeLimit, "specifies the maximum size of a transaction to be sent to the Espresso Network")
	f.StringSlice(prefix+".init-batcher-addresses", DefaultEspressoBatchPosterConfig.InitBatcherAddresses, "specifies the init batcher addresses")
	espressotee.AddEspressoRegisterServiceConfigOptions(prefix+".register-service-config", f)
}

var DefaultEspressoBatchPosterConfig = EspressoBatchPosterConfig{
	// We should send to espresso at a speed faster than the speed nitro is producing messages
	TxnsSendingInterval:        125 * time.Millisecond,
	TxnsResubmissionInterval:   2 * time.Second,
	ResubmitEspressoTxDeadline: 10 * time.Minute,
	HotShotUrl:                 "",
	TeeType:                    "NITRO",
	RegisterServiceConfig:      espressotee.DefaultEspressoRegisterServiceConfig,
	// EspressoTxSizeLimit is 1 MB, to have some buffer we set it to 900 KB
	TxSizeLimit:              900 * 1024,
	UserDataAttestationFile:  "",
	QuoteFile:                "",
	AttestationServiceURL:    "",
	HotShotFirstPostingBlock: 1,
	InitBatcherAddresses:     []string{},
	EventPollingStep:         100,
	AddressValidRanges:       []AddressValidRangeConfig{},
}

var TestEspressoBatchPosterConfig = EspressoBatchPosterConfig{
	TxnsSendingInterval:        time.Second,
	TxnsResubmissionInterval:   2 * time.Second,
	ResubmitEspressoTxDeadline: 10 * time.Second,
	HotShotUrl:                 "",
	TeeType:                    "TESTS",
	RegisterServiceConfig:      espressotee.DefaultEspressoRegisterServiceConfig,
	TxSizeLimit:                200 * 1024,

	HotShotFirstPostingBlock: 1,
	InitBatcherAddresses:     []string{},
	EventPollingStep:         100,
	AddressValidRanges:       []AddressValidRangeConfig{},
}
