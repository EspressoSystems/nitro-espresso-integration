package arbtest

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/log"

	hotshot_listener "github.com/offchainlabs/nitro/espresso/hotshot-listener"
	"github.com/offchainlabs/nitro/util/testhelpers"
)

const (
	SEQUENCER_API_WEBSOCKERT_URL = "ws://127.0.0.1:41000/v1"
)

func TestEspressoHotshotListener(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder, cleanup := createL1AndL2Node(ctx, t, true, false)
	defer cleanup()

	err := waitForL1Node(ctx)
	Require(t, err)
	shutdown := runEspresso()
	defer shutdown()

	logHandler := testhelpers.InitTestLog(t, log.LevelInfo)
	_ = logHandler

	// Wait for espresso node to be up
	err = waitForEspressoNode(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	log.Info("Starting the hotshot listener")

	// Deploy a RollupSequencerManager contract
	rollupSequencerManagerAddress := builder.L1Info.GetAddress("RollupSequencerManager")
	// Get Sequencer Address
	sequencerAddress := builder.L1Info.GetAddress("Sequencer")
	listener, err := hotshot_listener.NewHotshotListener(SEQUENCER_API_WEBSOCKERT_URL, rollupSequencerManagerAddress.Hex(), builder.L1.Client, sequencerAddress.Hex())
	if err != nil {
		t.Fatal(err)
	}

	// Add retries to the listener start function
	err = listener.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Note: These are rudimentary tests to check if the initial basic functionality
	// of the listener works. These tests will be modified in the future to include more processing checks

	// Check that we were able to find the quorum proposal and DA proposal for a given view and
	// are now processing it
	err = waitForWith(ctx, 1*time.Minute, 5*time.Second, func() bool {
		return logHandler.WasLogged("processing builder commitment and view number")
	})
	Require(t, err)

	// Check that node detected that it needs to process the next view
	err = waitForWith(ctx, 1*time.Minute, 5*time.Second, func() bool {
		return logHandler.WasLogged("next view is this node's view")
	})
	Require(t, err)

	// Check that decide event log was also processed
	err = waitForWith(ctx, 1*time.Minute, 5*time.Second, func() bool {
		return logHandler.WasLogged("processing leaf chain")
	})
	Require(t, err)

	// Stop and wait for the listener to stop
	listener.StopAndWait()
}
