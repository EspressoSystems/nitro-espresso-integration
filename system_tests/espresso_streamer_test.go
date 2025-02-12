package arbtest

import (
	"context"
	"testing"
	"time"

	"github.com/offchainlabs/nitro/arbstate"
	"github.com/offchainlabs/nitro/util/headerreader"
)

func createEspressoStreamer(builder *NodeBuilder) *arbstate.EspressoStreamer {

	namespace := builder.chainConfig.ChainID.Uint64()
	hotshotUrl := "http://127.0.0.1:41000"
	nextHotshotBlockNum := uint64(1)
	return arbstate.NewEspressoStreamer(
		namespace,
		[]string{hotshotUrl, hotshotUrl},
		nextHotshotBlockNum,
		1*time.Second,
		250*time.Millisecond,
		builder.L1Info.GetAddress("Bridge"),
		"http://0.0.0.0:8545",
		headerreader.DefaultConfig,
		builder.L1Info.GetAddress("EspressoTEEVerifierMock"),
	)
}

func TestEspressoStreamer(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder, cleanup := createL1AndL2Node(ctx, t, true)
	defer cleanup()

	cleanEspresso := runEspresso()
	defer cleanEspresso()
	espressoStreamer := createEspressoStreamer(builder)
	// Test PopMessageWithMetadataAndPos

	//Test PeekMessageWithMetadataAndPos returns nil if the queue is empty
	t.Run("Test PeekMessageWithMetadataAndPos returns nil if the queue is empty", func(t *testing.T) {
		messageWithMetadataAndPos := espressoStreamer.PeekMessageWithMetadataAndPos()
		if messageWithMetadataAndPos != nil {
			t.Errorf("Expected nil, got %v", messageWithMetadataAndPos)
		}
	})

	t.Run("Test PopMessageWithMetadataAndPos returns nil if the queue is empty", func(t *testing.T) {
		// TODO: implement test
		messageWithMetadataAndPos := espressoStreamer.PopMessageWithMetadataAndPos()
		if messageWithMetadataAndPos != nil {
			t.Errorf("Expected nil, got %v", messageWithMetadataAndPos)
		}
	})

	t.Run("Test PeekMessageWithMetadataAndPos peeks the first message from the queue", func(t *testing.T) {
		// add a new message to the queue
		// TODO: implement test
	})
	t.Run("Test PopMessageWithMetadataAndPos pops the first message from the queue", func(t *testing.T) {
		// TODO: implement test

		// create a new EspressoStreamer

	})

}
