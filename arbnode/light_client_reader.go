package arbnode

import (
	lightclient "github.com/EspressoSystems/espresso-sequencer-go/light-client"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
)

type LightClientReader struct {
	LightClient lightclient.Lightclient
}

func NewLightClientReader(lightClientAddr common.Address, l1client bind.ContractBackend) (*LightClientReader, error) {
	lightclient, err := lightclient.NewLightclient(lightClientAddr, l1client)
	if err != nil {
		return nil, err
	}

	return &LightClientReader{
		LightClient: *lightclient,
	}, nil
}

// Returns the most recent block height validated by the light client contract
func (l *LightClientReader) BlockHeight() (uint64, error) {
	return 0, nil
}
