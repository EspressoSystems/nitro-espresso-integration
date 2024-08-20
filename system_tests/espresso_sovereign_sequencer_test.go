package arbtest

import (
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/solgen/go/rollupgen"
	"github.com/offchainlabs/nitro/solgen/go/upgrade_executorgen"
	"math/big"
	"strings"
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

	// sequencer config
	builder.nodeConfig.Sequencer = true
	builder.nodeConfig.Dangerous.NoSequencerCoordinator = true
	builder.execConfig.Sequencer.Enable = true
	// using the sovereign sequencer
	builder.execConfig.Sequencer.Espresso = false
	builder.execConfig.Sequencer.EnableEspressoSovereign = true

	// transaction stream config
	builder.nodeConfig.TransactionStreamer.SovereignSequencerEnabled = true
	builder.nodeConfig.TransactionStreamer.EspressoNamespace = builder.chainConfig.ChainID.Uint64()
	builder.nodeConfig.TransactionStreamer.HotShotUrl = hotShotUrl

	cleanup := builder.Build(t)

	mnemonic := "indoor dish desk flag debris potato excuse depart ticket judge file exit"
	err := builder.L1Info.GenerateAccountWithMnemonic("CommitmentTask", mnemonic, 5)
	Require(t, err)
	builder.L1.TransferBalance(t, "Faucet", "CommitmentTask", big.NewInt(9e18), builder.L1Info)

	// Fund the stakers
	builder.L1Info.GenerateAccount("Staker1")
	builder.L1.TransferBalance(t, "Faucet", "Staker1", big.NewInt(9e18), builder.L1Info)
	builder.L1Info.GenerateAccount("Staker2")
	builder.L1.TransferBalance(t, "Faucet", "Staker2", big.NewInt(9e18), builder.L1Info)
	builder.L1Info.GenerateAccount("Staker3")
	builder.L1.TransferBalance(t, "Faucet", "Staker3", big.NewInt(9e18), builder.L1Info)

	// Update the rollup
	deployAuth := builder.L1Info.GetDefaultTransactOpts("RollupOwner", ctx)
	upgradeExecutor, err := upgrade_executorgen.NewUpgradeExecutor(builder.L2.ConsensusNode.DeployInfo.UpgradeExecutor, builder.L1.Client)
	Require(t, err)
	rollupABI, err := abi.JSON(strings.NewReader(rollupgen.RollupAdminLogicABI))
	Require(t, err)

	setMinAssertPeriodCalldata, err := rollupABI.Pack("setMinimumAssertionPeriod", big.NewInt(0))
	Require(t, err, "unable to generate setMinimumAssertionPeriod calldata")
	tx, err := upgradeExecutor.ExecuteCall(&deployAuth, builder.L2.ConsensusNode.DeployInfo.Rollup, setMinAssertPeriodCalldata)
	Require(t, err, "unable to set minimum assertion period")
	_, err = builder.L1.EnsureTxSucceeded(tx)
	Require(t, err)

	// Add the stakers into the validator whitelist
	staker1Addr := builder.L1Info.GetAddress("Staker1")
	staker2Addr := builder.L1Info.GetAddress("Staker2")
	staker3Addr := builder.L1Info.GetAddress("Staker3")
	setValidatorCalldata, err := rollupABI.Pack("setValidator", []common.Address{staker1Addr, staker2Addr, staker3Addr}, []bool{true, true, true})
	Require(t, err, "unable to generate setValidator calldata")
	tx, err = upgradeExecutor.ExecuteCall(&deployAuth, builder.L2.ConsensusNode.DeployInfo.Rollup, setValidatorCalldata)
	Require(t, err, "unable to set validators")
	_, err = builder.L1.EnsureTxSucceeded(tx)
	Require(t, err)

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

	err = checkTransferTxOnL2(t, ctx, builder.L2, "User14", builder.L2Info)
	Require(t, err)

	msgCnt, err := builder.L2.ConsensusNode.TxStreamer.GetMessageCount()
	Require(t, err)
	log.Info("msgCnt", "msgCnt", msgCnt)
	err = waitForWith(t, ctx, 6*time.Minute, 60*time.Second, func() bool {
		validatedCnt := builder.L2.ConsensusNode.BlockValidator.Validated(t)
		return validatedCnt == msgCnt
	})
	Require(t, err)
}
