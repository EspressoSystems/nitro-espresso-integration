package arbtest

import (
	"context"
	"testing"
	"time"

	lightclient "github.com/EspressoSystems/espresso-network/sdks/go/light-client"

	"github.com/ethereum/go-ethereum/common"
)

func TestEspressoSovereignSequencer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	valNodeCleanup := createValidationNode(ctx, t, true)
	defer valNodeCleanup()

	builder, cleanup := createL1AndL2Node(ctx, t, true, false)
	defer cleanup()

	err := waitForL1Node(ctx)
	Require(t, err)

	cleanEspresso := runEspresso()
	defer cleanEspresso()

	// wait for the builder
	err = waitForEspressoNode(ctx)
	Require(t, err)

	// create light client reader
	lightClientReader, err := lightclient.NewLightClientReader(common.HexToAddress(lightClientAddress), builder.L1.Client)

	Require(t, err)

	// wait for hotshot liveness
	err = waitForHotShotLiveness(ctx, lightClientReader)
	Require(t, err)
	err = checkTransferTxOnL2(t, ctx, builder.L2, "User14", builder.L2Info)
	Require(t, err)

	msgCnt, err := builder.L2.ConsensusNode.TxStreamer.GetMessageCount()
	Require(t, err)

	err = waitForWith(ctx, 8*time.Minute, 60*time.Second, func() bool {
		validatedCnt := builder.L2.ConsensusNode.BlockValidator.Validated(t)
		return validatedCnt >= msgCnt
	})
	Require(t, err)
}
