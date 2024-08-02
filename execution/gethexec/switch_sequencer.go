package gethexec

import (
	"context"
	"time"

	lightClient "github.com/EspressoSystems/espresso-sequencer-go/light-client"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

const (
	SequencingMode_Espresso    = 0
	SequencingMode_Centralized = 1
)

type SwitchSequencer struct {
	stopwaiter.StopWaiter

	centralized *Sequencer
	espresso    *EspressoSequencer

	switchPollInterval   time.Duration
	swtichDelayThreshold uint64
	lightClient          lightClient.LightClientReaderInterface

	mode int
}

func NewSwitchSequencer(centralized *Sequencer, espresso *EspressoSequencer, l1client bind.ContractBackend, configFetcher SequencerConfigFetcher) (*SwitchSequencer, error) {
	config := configFetcher()
	err := config.Validate()
	if err != nil {
		return nil, err
	}

	var lightclient lightClient.LightClientReaderInterface
	if config.LightClientAddress != "" {
		lightclient, err = lightClient.NewLightClientReader(common.HexToAddress(config.LightClientAddress), l1client)
		if err != nil {
			return nil, err
		}
	}

	return &SwitchSequencer{
		centralized:          centralized,
		espresso:             espresso,
		lightClient:          lightclient,
		mode:                 SequencingMode_Espresso,
		switchPollInterval:   config.SwitchPollInterval,
		swtichDelayThreshold: config.SwitchDelayThreshold,
	}, nil
}

func (s *SwitchSequencer) IsRunningEspressoMode() bool {
	return s.mode == SequencingMode_Espresso
}

func (s *SwitchSequencer) SwitchToEspresso(ctx context.Context) error {
	if s.IsRunningEspressoMode() {
		return nil
	}
	log.Info("Switching to espresso sequencer")

	s.mode = SequencingMode_Espresso

	s.centralized.StopAndWait()
	return s.espresso.Start(ctx)
}

func (s *SwitchSequencer) SwitchToCentralized(ctx context.Context) error {
	if !s.IsRunningEspressoMode() {
		return nil
	}
	s.mode = SequencingMode_Centralized
	log.Info("Switching to centrialized sequencer")

	s.espresso.StopAndWait()
	return s.centralized.Start(ctx)
}

func (s *SwitchSequencer) getRunningSequencer() TransactionPublisher {
	if s.IsRunningEspressoMode() {
		return s.espresso
	}
	return s.centralized
}

func (s *SwitchSequencer) PublishTransaction(ctx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	return s.getRunningSequencer().PublishTransaction(ctx, tx, options)
}

func (s *SwitchSequencer) CheckHealth(ctx context.Context) error {
	return s.getRunningSequencer().CheckHealth(ctx)
}

func (s *SwitchSequencer) Initialize(ctx context.Context) error {
	err := s.centralized.Initialize(ctx)
	if err != nil {
		return err
	}

	return s.espresso.Initialize(ctx)
}

func (s *SwitchSequencer) Start(ctx context.Context) error {
	s.StopWaiter.Start(ctx, s)
	err := s.getRunningSequencer().Start(ctx)
	if err != nil {
		return err
	}

	if s.lightClient != nil {
		s.CallIteratively(func(ctx context.Context) time.Duration {
			isLive, err := s.lightClient.IsHotShotLive(s.swtichDelayThreshold)
			log.Info("isLive status", "IsLive", isLive, "EspressoMode", s.IsRunningEspressoMode())
			if err != nil {
				return s.switchPollInterval
			}
			if s.IsRunningEspressoMode() && !isLive {
				log.Info("Switching to centralized sequencer")
				err = s.SwitchToCentralized(ctx)
			} else if !s.IsRunningEspressoMode() && isLive {
				log.Info("Switching to espresso sequencer")
				err = s.SwitchToEspresso(ctx)
			}

			if err != nil {
				log.Error("Error swithcing mode", "err", err)
				return 0
			}
			return s.switchPollInterval
		})
	}

	return nil
}

func (s *SwitchSequencer) StopAndWait() {
	s.getRunningSequencer().StopAndWait()
	s.StopWaiter.StopAndWait()
}

func (s *SwitchSequencer) Started() bool {
	return s.getRunningSequencer().Started()
}
