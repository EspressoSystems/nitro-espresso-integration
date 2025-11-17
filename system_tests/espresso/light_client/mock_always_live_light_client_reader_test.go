package light_client_test

import (
	"testing"

	light_client "github.com/offchainlabs/nitro/system_tests/espresso/light_client"
)

// TestMockAlwaysLiveLightClientReader tests the MockAlwaysLiveLightClientReader
// to ensure that it always returns true for IsHotShotLive, simulating a
// scenario where Hot Shot is always live.
func TestMockAlwaysLiveLightClientReader(t *testing.T) {
	// This test ensures that the MockAlwaysLiveLightClientReader always returns true
	// for IsHotShotLive, simulating a scenario where the light client is always live.
	client := light_client.NewMockAlwaysLiveLightClientReader()

	isLive, err := client.IsHotShotLive(0)
	if err != nil {
		t.Fatalf("isHotShotLive is not expected to ever return an error:have:\n\t%v", err)
	}

	if have, want := isLive, true; have != want {
		t.Fatalf("expected IsHotShotLive to return true:\nhave:\n\t%v\nwant:\n\t%v", have, want)
	}
}
