package arbnode

import (
	"time"

	"github.com/spf13/pflag"
)

// EspressoTxSizeLimit is 1 MB, to have some buffer we set it to 900 KB
const EspressoTxSizeLimit int64 = 900 * 1024

type EspressoBatchPosterConfig struct {
	TeeType                    string        `koanf:"tee-type"`
	HotShotUrl                 string        `koanf:"hotshot-url"`
	TxnsMonitoringInterval     time.Duration `koanf:"txns-monitoring-interval"`
	ResubmitEspressoTxDeadline time.Duration `koanf:"resubmit-espresso-tx-deadline"`
	AttestationServiceURL      string        `koanf:"attestation-service-url"`

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
	f.Duration(prefix+".resubmit-espresso-tx-deadline", DefaultEspressoBatchPosterConfig.ResubmitEspressoTxDeadline, "time threshold after which a transaction will be automatically resubmitted if no response is received")
	f.Duration(prefix+".txns-monitoring-interval", DefaultEspressoBatchPosterConfig.TxnsMonitoringInterval, "time threshold after which a transaction will be automatically resubmitted if no response is received")
	f.String(prefix+".attestation-service-url", DefaultEspressoBatchPosterConfig.AttestationServiceURL, "URL of the attestation service to use for obtaining zk proof over  attestation")
	f.StringSlice(prefix+".init-batcher-addresses", DefaultEspressoBatchPosterConfig.InitBatcherAddresses, "specifies the init batcher addresses")
}

var DefaultEspressoBatchPosterConfig = EspressoBatchPosterConfig{
	TxnsMonitoringInterval:     125 * time.Second,
	ResubmitEspressoTxDeadline: 10 * time.Minute,
	HotShotUrl:                 "",
	TeeType:                    "NITRO",
	AttestationServiceURL:      "",
	HotShotFirstPostingBlock:   1,
	InitBatcherAddresses:       []string{},
	EventPollingStep:           100,
	AddressValidRanges:         []AddressValidRangeConfig{},
}

var TestEspressoBatchPosterConfig = EspressoBatchPosterConfig{
	TxnsMonitoringInterval: 2 * time.Second,
	HotShotUrl:             "",
	TeeType:                "TESTS",

	HotShotFirstPostingBlock: 1,
	InitBatcherAddresses:     []string{},
	EventPollingStep:         100,
	AddressValidRanges:       []AddressValidRangeConfig{},
}
