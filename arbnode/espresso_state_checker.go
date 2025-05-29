package arbnode

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/util/stopwaiter"
	flag "github.com/spf13/pflag"
)

type StateCheckerConfig struct {
	Enable          bool          `koanf:"enable"`
	PollingInterval time.Duration `koanf:"polling-interval"`

	// http endpoint of the trusted node
	TrustedNodeUrl string `koanf:"trusted-node-url"`
}

var DefaultStateCheckerConfig = StateCheckerConfig{
	Enable:          true,
	PollingInterval: time.Second * 100,
}

func EspressoStateCheckerConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Bool(prefix+".enable", DefaultStateCheckerConfig.Enable, "enable state checker")
	f.Duration(prefix+".polling-interval", DefaultStateCheckerConfig.PollingInterval, "time after a success")
	f.String(prefix+".trusted-node-url", DefaultStateCheckerConfig.TrustedNodeUrl, "http endpoint of the trusted node")
}

type StateChecker struct {
	stopwaiter.StopWaiter

	config       StateCheckerConfig
	fatalErrChan chan error

	trustedClient *ethclient.Client
	myClient      *ethclient.Client
}

func NewStateChecker(
	config StateCheckerConfig,
	httpPort int,
	fatalErrChan chan error,
) *StateChecker {
	if config.TrustedNodeUrl == "" {
		return nil
	}

	client, err := ethclient.DialContext(context.Background(), config.TrustedNodeUrl)
	if err != nil {
		panic(err)
	}
	myUrl := fmt.Sprintf("http://localhost:%d", httpPort)
	myClient, err := ethclient.DialContext(context.Background(), myUrl)
	if err != nil {
		panic(err)
	}

	return &StateChecker{
		config:        config,
		fatalErrChan:  fatalErrChan,
		myClient:      myClient,
		trustedClient: client,
	}
}

func (s *StateChecker) Start(ctx context.Context) error {
	s.StopWaiter.Start(ctx, s)

	return s.CallIterativelySafe(func(ctx context.Context) time.Duration {
		err := s.checkState(ctx)
		if err != nil {
			log.Error("error checking state", "err", err)
			return s.config.PollingInterval
		}
		return s.config.PollingInterval
	})
}

func (s *StateChecker) checkState(ctx context.Context) error {
	block, err := s.trustedClient.BlockByNumber(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to get latest block through trusted node: %w", err)
	}
	blockNumber := block.Number()
	myBlock, err := s.myClient.BlockByNumber(ctx, blockNumber)
	if err != nil {
		return fmt.Errorf("failed to get block by number through my node: %w", err)
	}

	if block.Hash() != myBlock.Hash() {
		err := fmt.Errorf("block hash mismatch: trusted node: %s, my node: %s", block.Hash(), myBlock.Hash())
		s.fatalErrChan <- err
		return err
	}
	return nil
}
