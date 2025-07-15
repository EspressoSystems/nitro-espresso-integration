package submitter

import (
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/espresso"
	espresso_key_manager "github.com/offchainlabs/nitro/espresso/key-manager"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type MessageGetter interface {
	GetMessage(seqNum arbutil.MessageIndex) (*arbostypes.MessageWithMetadata, error)
}

type EspressoSubmitter interface {
	Start(sw *stopwaiter.StopWaiter) error
	NotifyNewPendingMessages(pos arbutil.MessageIndex, messages []arbostypes.MessageWithMetadataAndBlockInfo) error
	GetKeyManager() espresso_key_manager.EspressoKeyManagerInterface

	IsEscapeHatchEnabled() bool
	GetLastConfirmedPosition() (*arbutil.MessageIndex, error)
}

type EspressoSubmitterConfig struct {
	// Simple Configuration values. These will be expected to have default
	// values set for them, but can be overridden by the user.

	ChainID                               uint64
	EspressoTxnsPollingInterval           time.Duration
	EspressoTxnSubmissionInterval         time.Duration
	MaxBlockLagBeforeEscapeHatch          uint64
	EspressoMaxTransactionSize            int64
	ResubmitEspressoTxDeadline            time.Duration
	InitialFinalizedSequencerMessageCount *big.Int
	UseEscapeHatch                        bool
	EscapeHatchEnabled                    bool

	// These are attestation values that will signify information to load
	// for attestation initialization

	UserDataAttestationFile string
	QuoteFile               string

	// These are the interfaces that will be used to interact with
	// Espresso, and the underlying chain information.

	EspressoClient    espresso.TransactionStreamerEspressoClient
	LightClientReader espresso.TransactionStreamerLightClientReadeInterface
	KeyManager        espresso_key_manager.EspressoKeyManagerInterface
	MessageGetter     MessageGetter
	Db                ethdb.Database
}

var DefaultEspressoSubmitterConfig = EspressoSubmitterConfig{
	ChainID:                               0,
	EspressoTxnsPollingInterval:           time.Second,
	EspressoTxnSubmissionInterval:         time.Second,
	MaxBlockLagBeforeEscapeHatch:          350,
	EspressoMaxTransactionSize:            200_000,
	ResubmitEspressoTxDeadline:            16 * time.Second,
	InitialFinalizedSequencerMessageCount: big.NewInt(0),
}

type EspressoSubmitterConfigOption func(*EspressoSubmitterConfig)

func WithMultipleOptions(options ...EspressoSubmitterConfigOption) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		for _, option := range options {
			option(config)
		}
	}
}

func WithChainID(chainID uint64) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.ChainID = chainID
	}
}

func WithMessageGetter(getter MessageGetter) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.MessageGetter = getter
	}
}

func WithDatabase(db ethdb.Database) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.Db = db
	}
}

func WithAttestationFiles(userDataAttestationFile, quoteFile string) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.UserDataAttestationFile = userDataAttestationFile
		config.QuoteFile = quoteFile
	}
}

func WithEspressoClient(client espresso.TransactionStreamerEspressoClient) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.EspressoClient = client
	}
}

func WithLightClientReader(reader espresso.TransactionStreamerLightClientReadeInterface) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.LightClientReader = reader
	}
}

func WithKeyManager(keyManager espresso_key_manager.EspressoKeyManagerInterface) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.KeyManager = keyManager
	}
}

func WithMaxTransactionSize(size int64) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.EspressoMaxTransactionSize = size
	}
}

func WithTxnsSubmissionInterval(interval time.Duration) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.EspressoTxnSubmissionInterval = interval
	}
}

func WithTxnsPollingInterval(interval time.Duration) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.EspressoTxnsPollingInterval = interval
	}
}

func WithMaxBlockLagBeforeEscapeHatch(lag uint64) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.MaxBlockLagBeforeEscapeHatch = lag
	}
}

func WithInitialFinalizedSequencerMessageCount(count *big.Int) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.InitialFinalizedSequencerMessageCount = count
	}
}

func WithResubmitEspressoTxDeadline(deadline time.Duration) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.ResubmitEspressoTxDeadline = deadline
	}
}

func WithUseEscapeHatch(enable bool) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.UseEscapeHatch = enable
	}
}

func WithEnableEscapeHatch(enable bool) EspressoSubmitterConfigOption {
	return func(config *EspressoSubmitterConfig) {
		config.EscapeHatchEnabled = enable
	}
}

func ValidateEspressoSubmitterConfig(config EspressoSubmitterConfig) error {
	if config.EspressoClient == nil {
		return fmt.Errorf("espresso client is not set")
	}

	if config.LightClientReader == nil {
		return fmt.Errorf("light client reader is not set")
	}

	if config.MessageGetter == nil {
		return fmt.Errorf("message getter is not set")
	}

	if config.Db == nil {
		return fmt.Errorf("database is not set")
	}

	if config.KeyManager == nil {
		return fmt.Errorf("espresso key manager is not set")
	}

	if config.ChainID == 0 {
		return fmt.Errorf("chain ID is not set")
	}

	if config.EspressoMaxTransactionSize <= 0 {
		return fmt.Errorf("espresso max transaction size must be greater than 0")
	}

	if config.EspressoTxnsPollingInterval <= 0 {
		return fmt.Errorf("espresso transactions polling interval must be greater than 0")
	}

	if config.EspressoTxnSubmissionInterval <= 0 {
		return fmt.Errorf("espresso transactions submission interval must be greater than 0")
	}

	return nil
}
