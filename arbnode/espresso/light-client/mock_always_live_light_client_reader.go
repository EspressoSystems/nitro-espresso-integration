package lightclient

import (
	"github.com/offchainlabs/nitro/arbnode/espresso"
)

// MockAlwaysLiveLightClientReader is a mock implementation of the
// arbnode.TransactionStreamerLightClientReadeInterface that always returns
// true for IsHotShotLive, simulating a scenario where the light client is
// always live.
type MockAlwaysLiveLightClientReader struct{}

var _ espresso.TransactionStreamerLightClientReadeInterface = (*MockAlwaysLiveLightClientReader)(nil)

// IsHotShotLive is a mock implementation that always returns true,
func (m *MockAlwaysLiveLightClientReader) IsHotShotLive(delayThreshold uint64) (bool, error) {
	return true, nil
}

func NewMockAlwaysLiveLightClientReader() *MockAlwaysLiveLightClientReader {
	return &MockAlwaysLiveLightClientReader{}
}
