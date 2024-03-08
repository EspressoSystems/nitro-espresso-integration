package arbnode

import (
	espressoTypes "github.com/EspressoSystems/espresso-sequencer-go/types"
)

type LightClientReader struct {
}

func NewLightClientReader() (*LightClientReader, error) {
	return &LightClientReader{}, nil
}

// Returns the L1 block number where the light client has validated a particular
// hotshot block number, waiting for that hotshot height to be validated if necessary.
func (l *LightClientReader) WaitForHotShotHeight(blockHeight uint64) (uint64, error) {
	// TODO: implement
	return 0, nil
}

func (l *LightClientReader) FetchMerkleRootAtL1Block(blockHeight uint64) (espressoTypes.BlockMerkleRoot, error) {
	return espressoTypes.Commitment{}, nil
}
