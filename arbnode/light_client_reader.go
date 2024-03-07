package arbnode

type LightClientReader struct {
}

func NewLightClientReader() (*LightClientReader, error) {
	return &LightClientReader{}, nil
}

// Returns the L1 block number representing the point at which the light client has validated a particular
// hotshot block number
func (l *LightClientReader) WaitForHotShotHeight(blockHeight uint64) (uint64, error) {
	// TODO: implement for real, need an easy way to fetch the light client contract address
	return 0, nil
}
