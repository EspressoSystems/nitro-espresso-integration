package arbtest

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/log"
)

func createEspressoFinalityNode(t *testing.T, builder *NodeBuilder) (*TestClient, func()) {
	nodeConfig := builder.nodeConfig
	execConfig := builder.execConfig
	// Disable the batch poster because it requires redis if enabled on the 2nd node
	nodeConfig.BatchPoster.Enable = false
	nodeConfig.Feed.Output.Enable = true
	nodeConfig.Feed.Output.Signed = true
	nodeConfig.BlockValidator.Enable = true
	nodeConfig.DelayedSequencer.Enable = true
	nodeConfig.DelayedSequencer.FinalizeDistance = 1
	nodeConfig.Sequencer = true
	nodeConfig.Dangerous.NoSequencerCoordinator = true
	execConfig.Sequencer.Enable = true
	execConfig.Sequencer.EnableEspressoFinalityNode = true
	execConfig.Sequencer.EspressoFinalityNodeConfig.Namespace = builder.chainConfig.ChainID.Uint64()
	execConfig.Sequencer.EspressoFinalityNodeConfig.StartBlock = 1
	execConfig.Sequencer.EspressoFinalityNodeConfig.HotShotUrl = hotShotUrl

	return builder.Build2ndNode(t, &SecondNodeParams{
		nodeConfig: nodeConfig,
		execConfig: execConfig,
	})
}

func TestEspressoFinalityNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	valNodeCleanup := createValidationNode(ctx, t, true)
	defer valNodeCleanup()

	builder, cleanup := createL1AndL2Node(ctx, t, true)
	defer cleanup()

	err := waitForL1Node(ctx)
	Require(t, err)

	cleanEspresso := runEspresso()
	defer cleanEspresso()

	// wait for the builder
	err = waitForEspressoNode(ctx)
	Require(t, err)

	err = checkTransferTxOnL2(t, ctx, builder.L2, "User14", builder.L2Info)
	Require(t, err)
	err = checkTransferTxOnL2(t, ctx, builder.L2, "User15", builder.L2Info)
	Require(t, err)

	msgCnt, err := builder.L2.ConsensusNode.TxStreamer.GetMessageCount()
	Require(t, err)

	// start the finality node
	builderEspressoFinalityNode, cleanupEspressoFinalityNode := createEspressoFinalityNode(t, builder)
	defer cleanupEspressoFinalityNode()

	// Here we are checking that the transaction streamer of the finality node
	// should get the same number of messages as the transaction streamer of the sequencer node
	// This would verify that the finality node has correctly processed the hotshot transactions
	// and is able to correctly create blocks
	err = waitForWith(ctx, 6*time.Minute, 5*time.Second, func() bool {
		msgCntFinalityNode, err := builderEspressoFinalityNode.ConsensusNode.TxStreamer.GetMessageCount()
		log.Info("Finality node  count", "msgCntFinalityNode", msgCntFinalityNode, "msgCnt", msgCnt)
		Require(t, err)
		return msgCntFinalityNode == msgCnt
	})
	Require(t, err)
}
