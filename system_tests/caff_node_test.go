package arbtest

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbnode"
	"github.com/offchainlabs/nitro/solgen/go/bridgegen"
)

func createCaffNode(ctx context.Context, t *testing.T, existing *NodeBuilder) (*TestClient, func()) {
	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	nodeConfig := builder.nodeConfig
	execConfig := builder.execConfig

	// Disable the batch poster because it requires redis if enabled on the 2nd node
	nodeConfig.BatchPoster.Enable = false
	nodeConfig.BlockValidator.Enable = false
	nodeConfig.DelayedSequencer.Enable = false
	nodeConfig.DelayedSequencer.FinalizeDistance = 1
	nodeConfig.Sequencer = false
	nodeConfig.Dangerous.NoSequencerCoordinator = true
	execConfig.Sequencer.Enable = false
	execConfig.ForwardingTarget = existing.l2StackConfig.IPCPath
	execConfig.SecondaryForwardingTarget = []string{}
	nodeConfig.EspressoCaffNode.Enable = true
	nodeConfig.EspressoCaffNode.Namespace = builder.chainConfig.ChainID.Uint64()
	nodeConfig.EspressoCaffNode.NextHotshotBlock = 1
	nodeConfig.EspressoCaffNode.EspressoSGXVerifierAddr = existing.L1Info.GetAddress("EspressoTEEVerifierMock").Hex()
	nodeConfig.EspressoCaffNode.BatchPosterAddr = "0xb386a74Dcab67b66F8AC07B4f08365d37495Dd23"
	nodeConfig.EspressoCaffNode.StateCheckerConfig = arbnode.StateCheckerConfig{
		PollingInterval:        time.Second * 1,
		ErrorToleranceDuration: time.Hour * 1, // Set it to a larger value. That makes the state checker not shut down
		TrustedNodeUrl:         fmt.Sprintf("http://localhost:%d", 8945),
	}

	nodeConfig.EspressoCaffNode.ForceInclusionCheckerConfig = arbnode.ForceInclusionCheckerConfig{
		RetryTime:                time.Second * 2,
		PollingInterval:          time.Second * 1,
		BlockThresholdTolerance:  20,
		SecondThresholdTolerance: 200,
		ErrorToleranceDuration:   time.Minute * 10,
	}

	// for testing, we can use the same hotshot url for both
	nodeConfig.EspressoCaffNode.HotShotUrls = []string{hotShotUrl, hotShotUrl, hotShotUrl, hotShotUrl}
	nodeConfig.EspressoCaffNode.RetryTime = time.Second * 1
	nodeConfig.EspressoCaffNode.HotshotPollingInterval = time.Millisecond * 100

	nodeConfig.ParentChainReader.Enable = true

	builder.l2StackConfig.HTTPPort = 8946

	cleanup := builder.BuildEspressoCaffNode(t, existing)
	return builder.L2, cleanup
}

func TestEspressoCaffNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	valNodeCleanup := createValidationNode(ctx, t, true)
	defer valNodeCleanup()

	builder, cleanup := createL1AndL2Node(ctx, t, true)
	defer cleanup()

	trustedPort := builder.l2StackConfig.HTTPPort

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

	newAccount := "User16"
	l2Info := builder.L2Info
	l2Info.GenerateAccount(newAccount)
	addr := l2Info.GetAddress(newAccount)

	// Transfer via the delayed inbox
	delayedTx := l2Info.PrepareTx("Owner", newAccount, 3e7, transferAmount, nil)
	builder.L1.SendWaitTestTransactions(t, []*types.Transaction{
		WrapL2ForDelayed(t, delayedTx, builder.L1Info, "Faucet", 100000),
	})

	err = waitForWith(ctx, 240*time.Second, 10*time.Second, func() bool {
		balance := builder.L2.GetBalance(t, addr)
		log.Info("waiting for balance", "account", newAccount, "addr", addr, "balance", balance)
		return balance.Cmp(transferAmount) >= 0
	})
	Require(t, err)

	log.Info("Starting the caff node")
	// start the node
	builderCaffNode, cleanupCaffNode := createCaffNode(ctx, t, builder)
	defer cleanupCaffNode()

	err = waitForWith(ctx, 10*time.Minute, 10*time.Second, func() bool {
		balance1 := builderCaffNode.GetBalance(t, builder.L2Info.GetAddress("User14"))
		balance2 := builderCaffNode.GetBalance(t, builder.L2Info.GetAddress("User15"))
		log.Info("waiting for balance", "account", "User14", "balance", balance1, "account", "User15", "balance", balance2)
		return balance1.Cmp(transferAmount) > 0 && balance2.Cmp(transferAmount) > 0
	})
	Require(t, err)

	err = waitForWith(ctx, 240*time.Second, 10*time.Second, func() bool {
		balance := builderCaffNode.GetBalance(t, addr)
		log.Info("waiting for balance", "account", newAccount, "addr", addr, "balance", balance)
		if balance.Cmp(transferAmount) >= 0 {
			log.Info("Balance has entered account", "balance", balance, "account", newAccount)
		}
		return balance.Cmp(transferAmount) >= 0
	})
	Require(t, err)

	rpcClient := builderCaffNode.Client.Client()
	startTime := time.Now()
	// Wait till we have two blocks created
	for {
		var lastBlock map[string]interface{}
		err = rpcClient.CallContext(ctx, &lastBlock, "eth_getBlockByNumber", "latest", false)
		Require(t, err)
		if lastBlock == nil {
			// fail
			t.Fatal("last block is nil")
		}
		log.Info("last block", "lastBlock", lastBlock)
		numberString, ok := lastBlock["number"].(string)
		if !ok {
			t.Fatal("number is not a string")
		}
		// convert number to uint
		number, err := strconv.ParseInt(numberString, 0, 64)
		Require(t, err)
		if number >= 3 {
			break
		}
		if time.Since(startTime) > 10*time.Minute {
			t.Fatal("timeout waiting for node to create blocks")
		}
		time.Sleep(time.Second * 5)
	}

	// Send transaction to CaffNode and it should works later
	err = checkTransferTxOnL2(t, ctx, builderCaffNode, "User17", builder.L2Info)
	Require(t, err)

	// start the trusted node
	// trustedPort := 9000
	// trustedCleanup := mockTrustedNode(t, ctx, trustedPort)
	// defer trustedCleanup()

	fatalErrChan := make(chan error)
	// Check the state checker
	port := builder.l2StackConfig.HTTPPort
	// Set the trusted node url to the L1 node
	// This is to simulate the trusted url returning a different block
	stateChecker := arbnode.NewStateChecker(
		arbnode.StateCheckerConfig{
			PollingInterval:        time.Second * 1,
			TrustedNodeUrl:         fmt.Sprintf("http://localhost:%d", trustedPort),
			ErrorToleranceDuration: time.Second * 100,
		},
		port,
		fatalErrChan,
	)
	// Start the monitoring task without initial checking
	err = stateChecker.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-fatalErrChan:
		t.Fatal(err)

	case <-time.After(100 * time.Second):
	}
}

func mockTrustedNode(t *testing.T, ctx context.Context, port int) func() {
	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	builder.l2StackConfig.HTTPPort = port
	builder.l2StackConfig.HTTPHost = "0.0.0.0"
	builder.useL2StackConfig = true
	return builder.BuildL2(t)
}

func TestEspressoForceInclusionChecker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, true)
	cleanup := builder.Build(t)
	defer cleanup()

	addr := builder.addresses.SequencerInbox
	seqInbox, err := bridgegen.NewSequencerInbox(addr, builder.L1.Client)
	if err != nil {
		t.Fatal(err)
	}

	mockSeqInbox := &MockSeqInbox{
		MaxDelayBlocks:  big.NewInt(20),
		MaxDelaySeconds: big.NewInt(200),
		seqInbox:        seqInbox,
	}

	config := arbnode.ForceInclusionCheckerConfig{
		RetryTime:                time.Second * 2,
		PollingInterval:          time.Second * 1,
		BlockThresholdTolerance:  20,
		SecondThresholdTolerance: 200,
		ErrorToleranceDuration:   time.Minute * 10,
	}

	delayedBridge, err := arbnode.NewDelayedBridge(builder.L1.Client, builder.addresses.Bridge, builder.addresses.DeployedAt)

	reader := builder.L2.ConsensusNode.L1Reader

	delayedMessageFetcher := arbnode.NewDelayedMessageFetcher(
		delayedBridge,
		reader,
		builder.L2.ConsensusNode.ArbDB,
		100,
		false,
		false,
		10,
		builder.L2.ConsensusNode.InboxReader,
	)

	fatalErrChan := make(chan error)

	forceInclusionChecker := arbnode.NewForceInclusionChecker(mockSeqInbox, config, reader, delayedMessageFetcher, fatalErrChan)
	forceInclusionChecker.Start(ctx)

	delayedTx := builder.L2Info.PrepareTx("Faucet", "Owner", 3e7, transferAmount, nil)
	builder.L1.SendWaitTestTransactions(t, []*types.Transaction{
		WrapL2ForDelayed(t, delayedTx, builder.L1Info, "Faucet", 100000),
	})

	select {
	case err := <-fatalErrChan:
		if err == nil {
			t.Fatal("expected an error from fatalErrChan, got nil")
		} else {
			t.Logf("received error as expected: %v", err)
		}
	case <-time.After(100 * time.Second):
		t.Fatal("did not receive error from fatalErrChan within timeout")
	}
}

// MockSeqInbox is a mock implementation of the sequencer inbox interface,
// allowing customizable time variation values for testing purposes.
// This is useful because the real contract hardcodes MaxTimeVariation when deployBold is disabled.
type MockSeqInbox struct {
	MaxDelayBlocks  *big.Int
	MaxDelaySeconds *big.Int
	seqInbox        *bridgegen.SequencerInbox
}

func (m *MockSeqInbox) MaxTimeVariation(ctx context.Context) (*big.Int, *big.Int, *big.Int, *big.Int, error) {
	return m.MaxDelayBlocks, nil, m.MaxDelaySeconds, nil, nil
}

func (m *MockSeqInbox) TotalDelayedMessagesRead(ctx context.Context) (*big.Int, error) {
	return m.seqInbox.TotalDelayedMessagesRead(&bind.CallOpts{Context: ctx})
}
