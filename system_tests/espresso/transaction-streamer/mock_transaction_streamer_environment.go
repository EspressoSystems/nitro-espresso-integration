package transaction_streamer

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/offchainlabs/nitro/arbnode"
	"github.com/offchainlabs/nitro/broadcaster"
	"github.com/offchainlabs/nitro/espresso"
	"github.com/offchainlabs/nitro/espresso/submitter"
	chain "github.com/offchainlabs/nitro/system_tests/espresso/chain"
	execution_engine "github.com/offchainlabs/nitro/system_tests/espresso/execution-engine"
	key_manager "github.com/offchainlabs/nitro/system_tests/espresso/key-manager"
	light_client "github.com/offchainlabs/nitro/system_tests/espresso/light-client"
)

// MockTransactionStreamerEnvironmentConfig is a configuration struct for
// creating a mock Transaction Streamer environment for testing purposes.
//
// It allows for custom configuration and setup of the testing environment
// with different configurable options.
type MockTransactionStreamerEnvironmentConfig struct {
	// Transaction Streamer Environment Configuration options
	Database                         ethdb.Database
	ChainConfig                      *params.ChainConfig
	Exec                             arbnode.TransactionStreamerExecutionSequencer
	BroadcastServer                  *broadcaster.Broadcaster
	FatalErrChan                     chan<- error
	TransactionStreamerConfigFetcher arbnode.TransactionStreamerConfigFetcher
	SnapSyncConfig                   *arbnode.SnapSyncConfig

	// Espresso Environment Configuration options
	EspressoClientOptions []TransactionStreamerEspressoClientOption

	// Submitter Options
	SubmitterOptions []submitter.EspressoSubmitterConfigOption
	SubmitterCreator func(options ...submitter.EspressoSubmitterConfigOption) (submitter.EspressoSubmitter, error)
}

// MockTransactionStreamerEnvironmentConfigDefault represents an option that
// allows for the modification of the MockTransactionStreamerEnvironmentConfig.
//
// This allows the caller to modify the configuration of the mock for specific
// testing scenarios.
type MockTransactionStreamerEnvironmentOption func(*MockTransactionStreamerEnvironmentConfig)

// TransactionStreamerEspressoClient is a function that modifies the
// TransactionStreamerEspressoClient in the
// MockTransactionStreamerEnvironmentConfig.
//
// This allows for the modification of the Espresso Client that is used in the
// TransactionStreamer environment.
//
// This is primarily utilized to add layers of functionality to the
// TransactionStreamerEspressoClient.
type TransactionStreamerEspressoClientOption func(input espresso.TransactionStreamerEspressoClient) espresso.TransactionStreamerEspressoClient

// ErrorFailedToCreateTransactionStreamer is an error type that indicates that
// the TransactionStreamer could not be created successfully.
type ErrorFailedToCreateTransactionStreamer struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToCreateTransactionStreamer) Error() string {
	return fmt.Sprintf("failed to create TransactionStreamer: %v", e.Cause)
}

// ErrorFailedToCreateEspressoSubmitter is an error type that indicates that
// the EspressoSubmitter could not be created successfully.
type ErrorFailedToCreateEspressoSubmitter struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToCreateEspressoSubmitter) Error() string {
	return fmt.Sprintf("failed to create EspressoSubmitter: %v", e.Cause)
}

// WithADatabase is a function that sets the Database in the
// MockTransactionStreamerEnvironmentConfig.
func WithDatabase(database ethdb.Database) MockTransactionStreamerEnvironmentOption {
	return func(config *MockTransactionStreamerEnvironmentConfig) {
		config.Database = database
	}
}

// WithChainConfig is a function that sets the ChainConfig in the
// MockTransactionStreamerEnvironmentConfig.
func WithChainConfig(chainConfig *params.ChainConfig) MockTransactionStreamerEnvironmentOption {
	return func(config *MockTransactionStreamerEnvironmentConfig) {
		config.ChainConfig = chainConfig
	}
}

// WithExecutionSequencer is a function that sets the ExecutionSequencer in the
// MockTransactionStreamerEnvironmentConfig.
func WithExecutionSequencer(exec arbnode.TransactionStreamerExecutionSequencer) MockTransactionStreamerEnvironmentOption {
	return func(config *MockTransactionStreamerEnvironmentConfig) {
		config.Exec = exec
	}
}

// WithBroadcastServer is a function that sets the BroadcastServer in the
// MockTransactionStreamerEnvironmentConfig.
func WithBroadcastServer(broadcastServer *broadcaster.Broadcaster) MockTransactionStreamerEnvironmentOption {
	return func(config *MockTransactionStreamerEnvironmentConfig) {
		config.BroadcastServer = broadcastServer
	}
}

// WithFatalErrChan is a function that sets the FatalErrChan in the
// MockTransactionStreamerEnvironmentConfig.
func WithFatalErrChan(fatalErrChan chan<- error) MockTransactionStreamerEnvironmentOption {
	return func(config *MockTransactionStreamerEnvironmentConfig) {
		config.FatalErrChan = fatalErrChan
	}
}

// WithTransactionStreamerConfigFetcher is a function that sets the
// TransactionStreamerConfigFetcher in the
// MockTransactionStreamerEnvironmentConfig.
func WithTransactionStreamerConfigFetcher(fetcher arbnode.TransactionStreamerConfigFetcher) MockTransactionStreamerEnvironmentOption {
	return func(config *MockTransactionStreamerEnvironmentConfig) {
		config.TransactionStreamerConfigFetcher = fetcher
	}
}

// WithSnapSyncConfig is a function that sets the SnapSyncConfig in the
// MockTransactionStreamerEnvironmentConfig.
func WithSnapSyncConfig(snapSyncConfig *arbnode.SnapSyncConfig) MockTransactionStreamerEnvironmentOption {
	return func(config *MockTransactionStreamerEnvironmentConfig) {
		config.SnapSyncConfig = snapSyncConfig
	}
}

// AddEspressoClientOptions is a function that adds options to the Espresso
// Client in the MockTransactionStreamerEnvironmentConfig.
func AddEspressoClientOptions(options ...TransactionStreamerEspressoClientOption) MockTransactionStreamerEnvironmentOption {
	return func(config *MockTransactionStreamerEnvironmentConfig) {
		config.EspressoClientOptions = append(config.EspressoClientOptions, options...)
	}
}

// AddEspressoClientOptions adds an option to the Espresso Client in the
// MockTransactionStreamerEnvironmentConfig.
func AddSubmitterOptions(
	options ...submitter.EspressoSubmitterConfigOption,
) MockTransactionStreamerEnvironmentOption {
	return func(config *MockTransactionStreamerEnvironmentConfig) {
		config.SubmitterOptions = append(config.SubmitterOptions, options...)
	}
}

// WithSubmitterCreator is a function that sets the SubmitterCreator in the
// MockTransactionStreamerEnvironmentConfig.
func WithSubmitterCreator(
	creator func(options ...submitter.EspressoSubmitterConfigOption) (submitter.EspressoSubmitter, error),
) MockTransactionStreamerEnvironmentOption {
	return func(config *MockTransactionStreamerEnvironmentConfig) {
		config.SubmitterCreator = creator
	}
}

// NewMockTransactionStreamerEnvironment creates a mock Transaction Streamer
// environment for testing purposes.
//
// The function operates in such a way that it allows for the passed options
// to modify the configuration of the mock environment (as needed), yet
// should still provides a default configuration that is suitable for
// most testing scenarios.
//
// Such a setup / approach allows for the consumer of the mock to only
// modify what is necessary for their specific test case.
//
// NOTE: This does not start the TransactionStreamer, start submitting messages
// to the TransactionStreamer, or start producing blocks in the mock Espresso
// Chain. Those actions are left to the caller of this function, so they are
// able to control the timing, and configuration of those actions.
func NewMockTransactionStreamerEnvironment(ctx context.Context, options ...MockTransactionStreamerEnvironmentOption) (*chain.MockEspressoChain, espresso.TransactionStreamerEspressoClient, submitter.EspressoSubmitter, *arbnode.TransactionStreamer, error) {
	// Setup the Default Configuration for the Mock Transaction Streamer Environment
	config := &MockTransactionStreamerEnvironmentConfig{
		Database:     rawdb.NewMemoryDatabase(),
		ChainConfig:  params.TestChainConfig,
		Exec:         execution_engine.NewMockExecutionEngine(execution_engine.DefaultMessageHasher),
		FatalErrChan: make(chan error),
		TransactionStreamerConfigFetcher: func() *arbnode.TransactionStreamerConfig {
			return &arbnode.DefaultTransactionStreamerConfig
		},

		SubmitterOptions: []submitter.EspressoSubmitterConfigOption{
			submitter.WithLightClientReader(light_client.NewMockAlwaysLiveLightClientReader()),
			submitter.WithKeyManager(key_manager.NewMockEspressoKeyManager()),
			submitter.WithMaxTransactionSize(1_000_000),
			submitter.WithTxnsPollingInterval(arbnode.DefaultBatchPosterConfig.EspressoTxnsPollingInterval),
			submitter.WithTxnsSubmissionInterval(arbnode.DefaultBatchPosterConfig.EspressoTxnsSubmissionInterval),
			submitter.WithResubmitEspressoTxDeadline(arbnode.DefaultBatchPosterConfig.ResubmitEspressoTxDeadline),
		},
		SubmitterCreator: func(options ...submitter.EspressoSubmitterConfigOption) (submitter.EspressoSubmitter, error) {
			return submitter.NewEspressoOriginalSubmitter(options...)
		},
	}

	// Apply the provided options to the configuration
	for _, option := range options {
		option(config)
	}

	// Create a mock Espresso Chain
	espressoChain := chain.NewMockEspressoChain()

	// Create and configure the Espresso Client
	var espressoClient espresso.TransactionStreamerEspressoClient = espressoChain
	for _, option := range config.EspressoClientOptions {
		espressoClient = option(espressoClient)
	}

	// Create the initial Streamer configuration
	streamer, err := arbnode.NewTransactionStreamer(
		ctx,
		config.Database,
		config.ChainConfig,
		config.Exec,
		config.BroadcastServer,
		config.FatalErrChan,
		config.TransactionStreamerConfigFetcher,
		config.SnapSyncConfig,
	)

	if err != nil {
		return nil, nil, nil, nil, ErrorFailedToCreateTransactionStreamer{Cause: err}
	}

	// Prepend the EspressoClient to the given list of SubmitterOptions.
	// This allows for any user passed option to override the EspressoClient
	// to still take affect.
	originalSubmiterOptions := config.SubmitterOptions
	config.SubmitterOptions = append(
		make([]submitter.EspressoSubmitterConfigOption, 0, len(config.SubmitterOptions)+2),
		submitter.WithEspressoClient(espressoClient),
		arbnode.WithTransactionStreamer(streamer),
	)
	config.SubmitterOptions = append(config.SubmitterOptions, originalSubmiterOptions...)

	// Create teh EspressoSubmitter with the provided options.
	espressoSubmitter, err := config.SubmitterCreator(config.SubmitterOptions...)
	if err != nil {
		return nil, nil, nil, nil, ErrorFailedToCreateEspressoSubmitter{Cause: err}
	}

	// Enable Espresso by setting the EspressoSubmitter in the
	// TransactionStreamer.
	arbnode.SetEspressoSubmitter(streamer, espressoSubmitter)
	return espressoChain, espressoClient, espressoSubmitter, streamer, nil
}
