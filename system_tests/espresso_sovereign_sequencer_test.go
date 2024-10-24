package arbtest

import (
	"context"
	"fmt"
	lightclient "github.com/EspressoSystems/espresso-sequencer-go/light-client"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"testing"
	"time"
)

func createL1AndL2Node(ctx context.Context, t *testing.T) (*NodeBuilder, func()) {
	builder := NewNodeBuilder(ctx).DefaultConfig(t, true)
	builder.l1StackConfig.HTTPPort = 8545
	builder.l1StackConfig.WSPort = 8546
	builder.l1StackConfig.HTTPHost = "0.0.0.0"
	builder.l1StackConfig.HTTPVirtualHosts = []string{"*"}
	builder.l1StackConfig.WSHost = "0.0.0.0"
	builder.l1StackConfig.DataDir = t.TempDir()
	builder.l1StackConfig.WSModules = append(builder.l1StackConfig.WSModules, "eth")
	builder.chainConfig.ArbitrumChainParams.EnableEspresso = true

	// poster config
	builder.nodeConfig.BatchPoster.Enable = true
	builder.nodeConfig.BatchPoster.ErrorDelay = 5 * time.Second
	builder.nodeConfig.BatchPoster.MaxSize = 41
	builder.nodeConfig.BatchPoster.PollInterval = 10 * time.Second
	builder.nodeConfig.BatchPoster.MaxDelay = -1000 * time.Hour
	builder.nodeConfig.BatchPoster.LightClientAddress = lightClientAddress
	builder.nodeConfig.BatchPoster.HotShotUrl = hotShotUrl

	// validator config
	builder.nodeConfig.BlockValidator.Enable = true
	builder.nodeConfig.BlockValidator.ValidationPoll = 2 * time.Second
	builder.nodeConfig.BlockValidator.ValidationServer.URL = fmt.Sprintf("ws://127.0.0.1:%d", arbValidationPort)
	builder.nodeConfig.BlockValidator.LightClientAddress = lightClientAddress
	builder.nodeConfig.BlockValidator.Espresso = true
	builder.nodeConfig.DelayedSequencer.Enable = true
	builder.nodeConfig.DelayedSequencer.FinalizeDistance = 1

	// sequencer config
	builder.nodeConfig.Sequencer = true
	builder.nodeConfig.ParentChainReader.Enable = true // This flag is necessary to enable sequencing transactions with espresso behavior
	builder.nodeConfig.Dangerous.NoSequencerCoordinator = true
	builder.execConfig.Sequencer.EnableEspressoSovereign = true
	builder.execConfig.Sequencer.Enable = true
	builder.execConfig.Sequencer.LightClientAddress = lightClientAddress

	// transaction stream config
	builder.nodeConfig.TransactionStreamer.SovereignSequencerEnabled = true
	builder.nodeConfig.TransactionStreamer.EspressoNamespace = builder.chainConfig.ChainID.Uint64()
	builder.nodeConfig.TransactionStreamer.HotShotUrl = hotShotUrl

	cleanup := builder.Build(t)

	mnemonic := "indoor dish desk flag debris potato excuse depart ticket judge file exit"
	err := builder.L1Info.GenerateAccountWithMnemonic("CommitmentTask", mnemonic, 5)
	Require(t, err)
	builder.L1.TransferBalance(t, "Faucet", "CommitmentTask", big.NewInt(9e18), builder.L1Info)

	return builder, cleanup
}

func TestSovereignSequencer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	valNodeCleanup := createValidationNode(ctx, t, true)
	defer valNodeCleanup()

	builder, cleanup := createL1AndL2Node(ctx, t)
	defer cleanup()

	err := waitForL1Node(t, ctx)
	Require(t, err)

	cleanEspresso := runEspresso(t, ctx)
	defer cleanEspresso()

	// wait for the builder
	err = waitForEspressoNode(t, ctx)
	Require(t, err)

	// create light client reader
	lightClientReader, err := lightclient.NewLightClientReader(common.HexToAddress(lightClientAddress), builder.L1.Client)

	Require(t, err)

	// wait for hotshot liveness
	err = waitForHotShotLiveness(t, ctx, lightClientReader)
	Require(t, err)
	err = checkTransferTxOnL2(t, ctx, builder.L2, "User14", builder.L2Info)
	Require(t, err)

	msgCnt, err := builder.L2.ConsensusNode.TxStreamer.GetMessageCount()
	Require(t, err)

	err = waitForWith(t, ctx, 6*time.Minute, 60*time.Second, func() bool {
		validatedCnt := builder.L2.ConsensusNode.BlockValidator.Validated(t)
		return validatedCnt == msgCnt
	})
	Require(t, err)
}
