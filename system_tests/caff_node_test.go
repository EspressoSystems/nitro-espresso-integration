package arbtest

import (
	"context"
	"errors"
	"math/big"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

func createCaffNode(ctx context.Context, t *testing.T, existing *NodeBuilder) (*NodeBuilder, func()) {
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
	nodeConfig.EspressoCaffNode.EspressoTEEVerifierAddr = existing.L1Info.GetAddress("EspressoTEEVerifierMock").Hex()
	// reuse the caff node settings so we can set them outside this function.
	nodeConfig.EspressoCaffNode.WaitForFinalization = existing.nodeConfig.EspressoCaffNode.WaitForFinalization
	nodeConfig.EspressoCaffNode.WaitForConfirmations = existing.nodeConfig.EspressoCaffNode.WaitForConfirmations
	nodeConfig.EspressoCaffNode.RequiredBlockDepth = existing.nodeConfig.EspressoCaffNode.RequiredBlockDepth

	// for testing, we can use the same hotshot url for both
	nodeConfig.EspressoCaffNode.HotShotUrls = []string{hotShotUrl, hotShotUrl, hotShotUrl, hotShotUrl}
	nodeConfig.EspressoCaffNode.RetryTime = time.Second * 1
	nodeConfig.EspressoCaffNode.HotshotPollingInterval = time.Millisecond * 100

	nodeConfig.ParentChainReader.Enable = true

	cleanup := builder.BuildEspressoCaffNode(t, existing)
	return builder, cleanup
}

func createCaffNodeConfig(ctx context.Context, t *testing.T) *NodeBuilder {
	builder := NewNodeBuilder(ctx).DefaultConfig(t, true)
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
	execConfig.SecondaryForwardingTarget = []string{}
	nodeConfig.EspressoCaffNode.Enable = true
	nodeConfig.EspressoCaffNode.Namespace = builder.chainConfig.ChainID.Uint64()
	nodeConfig.EspressoCaffNode.NextHotshotBlock = 1

	// for testing, we can use the same hotshot url for both
	nodeConfig.EspressoCaffNode.HotShotUrls = []string{hotShotUrl, hotShotUrl, hotShotUrl, hotShotUrl}
	nodeConfig.EspressoCaffNode.RetryTime = time.Second * 1
	nodeConfig.EspressoCaffNode.HotshotPollingInterval = time.Millisecond * 100

	nodeConfig.ParentChainReader.Enable = true

	return builder
}

func TestEspressoCaffNode(t *testing.T) {
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
	builder, cleanupCaffNode := createCaffNode(ctx, t, builder)
	builderCaffNode := builder.L2
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
}
func TestEspressoCaffNodeDelayedMessagesConfirmations(t *testing.T) {
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

	newAccount := "User16"
	l2Info := builder.L2Info
	l2Info.GenerateAccount(newAccount)
	addr := l2Info.GetAddress(newAccount)

	// Set caff node config variables
	builder.nodeConfig.EspressoCaffNode.WaitForConfirmations = true
	builder.nodeConfig.EspressoCaffNode.RequiredBlockDepth = 6
	builder.nodeConfig.EspressoCaffNode.WaitForFinalization = false

	// start the node
	log.Info("Starting the caff node")
	builder2, cleanupCaffNode := createCaffNode(ctx, t, builder)
	builderCaffNode := builder2.L2
	defer cleanupCaffNode()

	// Transfer via the delayed inbox
	delayedTx := l2Info.PrepareTx("Owner", newAccount, 3e7, transferAmount, nil)
	log.Info("Delayed tx", "delayedtx", delayedTx)
	tx := builder.L1.SendWaitTestTransactions(t, []*types.Transaction{
		WrapL2ForDelayed(t, delayedTx, builder.L1Info, "Faucet", 100000),
	})
	// Check the caff node RPC for tx. assert that it is not there.
	_, _, err = builderCaffNode.Client.TransactionByHash(ctx, tx[0].TxHash)
	ExpectErr(t, err, ethereum.NotFound)
	// Wait for the tx header to have required confirmations.
	err = waitForWith(ctx, 240*time.Second, 1*time.Second, func() bool {
		header, err := builder.L1.Client.HeaderByNumber(ctx, nil) // get the latest header to check tx block depth
		Require(t, err)
		return header.Number.Int64() >= tx[0].BlockNumber.Int64()+int64(builder.nodeConfig.EspressoCaffNode.RequiredBlockDepth) // check that the tx is at least RequiredBlockDepth blocks deep in the parent chains state.
	})
	// Wait for the tx to appear on the caff node
	err = waitForWith(ctx, 240*time.Second, 10*time.Second, func() bool {
		balance := builderCaffNode.GetBalance(t, addr)
		log.Info("waiting for balance", "account", newAccount, "addr", addr, "balance", balance)
		if balance.Cmp(transferAmount) >= 0 {
			log.Info("Balance has entered account", "balance", balance, "account", newAccount)
		}
		return balance.Cmp(transferAmount) >= 0
	})
	Require(t, err)
}
func TestEspressoCaffNodeDelayedMessagesFinalized(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	valNodeCleanup := createValidationNode(ctx, t, true)
	defer valNodeCleanup()

	builder, cleanup := createL1AndL2Node(ctx, t, true, false)
	defer cleanup()
	l1Info := builder.L1Info

	err := waitForL1Node(ctx)
	Require(t, err)

	cleanEspresso := runEspresso()
	defer cleanEspresso()

	// wait for the builder
	err = waitForEspressoNode(ctx)
	Require(t, err)

	newAccount := "User16"
	l2Info := builder.L2Info
	l2Info.GenerateAccount(newAccount)
	addr := l2Info.GetAddress(newAccount)
	// Set caff node config vars
	builder.nodeConfig.EspressoCaffNode.WaitForConfirmations = false
	builder.nodeConfig.EspressoCaffNode.RequiredBlockDepth = 6
	builder.nodeConfig.EspressoCaffNode.WaitForFinalization = true
	// start the node
	log.Info("Starting the caff node")
	builder2, cleanupCaffNode := createCaffNode(ctx, t, builder)
	builderCaffNode := builder2.L2
	defer cleanupCaffNode()

	// Transfer via the delayed inbox
	log.Info("l1info", "l1info", l1Info)
	delayedTx := l2Info.PrepareTx("Owner", newAccount, 3e7, transferAmount, nil)
	log.Info("Delayed tx", "delayedtx", delayedTx)
	tx := builder.L1.SendWaitTestTransactions(t, []*types.Transaction{
		WrapL2ForDelayed(t, delayedTx, builder.L1Info, "Faucet", 100000),
	})
	// Check the caff node RPC for tx. assert that it is not there.
	_, _, err = builderCaffNode.Client.TransactionByHash(ctx, tx[0].TxHash)
	ExpectErr(t, err, ethereum.NotFound)
	// Wait for the tx header to be finalized.
	err = waitForWith(ctx, 240*time.Second, 1*time.Second, func() bool {
		header, err := builder.L1.Client.HeaderByNumber(ctx, big.NewInt(rpc.FinalizedBlockNumber.Int64()))
		Require(t, err)
		return header.Number.Int64() >= tx[0].BlockNumber.Int64()
	})
	// Wait for the tx to appear on the caff node
	err = waitForWith(ctx, 240*time.Second, 10*time.Second, func() bool {
		balance := builderCaffNode.GetBalance(t, addr)
		log.Info("waiting for balance", "account", newAccount, "addr", addr, "balance", balance)
		if balance.Cmp(transferAmount) >= 0 {
			log.Info("Balance has entered account", "balance", balance, "account", newAccount)
		}
		return balance.Cmp(transferAmount) >= 0
	})
	Require(t, err)
}

func TestEspressoCaffNodeUnfinalizedDelayedMessages(t *testing.T) {
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

	newAccount := "User16"
	l2Info := builder.L2Info
	l2Info.GenerateAccount(newAccount)
	addr := l2Info.GetAddress(newAccount)
	// set caff node config vars
	builder.nodeConfig.EspressoCaffNode.WaitForConfirmations = false
	builder.nodeConfig.EspressoCaffNode.RequiredBlockDepth = 6
	builder.nodeConfig.EspressoCaffNode.WaitForFinalization = false

	// start the node
	log.Info("Starting the caff node")
	builder2, cleanupCaffNode := createCaffNode(ctx, t, builder)
	builderCaffNode := builder2.L2
	defer cleanupCaffNode()

	// Transfer via the delayed inbox
	delayedTx3 := l2Info.PrepareTx("Owner", newAccount, 3e7, transferAmount, nil)
	tx3 := builder.L1.SendWaitTestTransactions(t, []*types.Transaction{
		WrapL2ForDelayed(t, delayedTx3, builder.L1Info, "Faucet", 100000),
	})
	// Wait for the tx to appear on the caff node
	err = waitForWith(ctx, 240*time.Second, 10*time.Second, func() bool {
		balance := builderCaffNode.GetBalance(t, addr)
		log.Info("waiting for balance", "account", newAccount, "addr", addr, "balance", balance)
		if balance.Cmp(transferAmount) >= 0 {
			log.Info("Balance has entered account", "balance", balance, "account", newAccount)
		}
		return balance.Cmp(transferAmount) >= 0
	})
	Require(t, err)

	finalizedHeader, err := builder.L1.Client.HeaderByNumber(ctx, big.NewInt(rpc.FinalizedBlockNumber.Int64()))
	if tx3[0].BlockNumber.Int64() <= finalizedHeader.Number.Int64() {
		t.Fatal("Tx finalized before appearing in the caff node")
	}

}

// RequireErr:
// This serves to assert that we should be expecting some error during the test, and if there is not an error, fail the test.
func RequireErr(t *testing.T, err error, expectedError error) {
	t.Helper()
	if err == nil {
		log.Error("expected an error to occurr", "expected error", expectedError)
		t.Fatal(err, expectedError)
	}
}

// ExpectErr:
// This serves to assert that we should be expecting a specific error during the test, and if the error does not match, fail the test.
func ExpectErr(t *testing.T, err error, expectedError error) {
	t.Helper()
	if !errors.Is(err, expectedError) {
		log.Error("expected specific error to occurr", "expected error", expectedError, "actual error", err)
		t.Fatal(err, expectedError)
	}
}

// This tests that the caff node config validates that known versions of arb sequencers are not enabled if the caff node is.
func TestCaffNodeConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	builder := createCaffNodeConfig(ctx, t)
	err := builder.nodeConfig.Validate()
	Require(t, err)

	expectedErr := errors.New("cannot start a Caff node with any sequencer enabled")
	// Test if this node is attempting to be a sequencer
	builder.nodeConfig.Sequencer = true
	err = builder.nodeConfig.Validate()
	RequireErr(t, err, expectedErr)
	// Test the delayed sequencer
	builder.nodeConfig.Sequencer = false
	builder.nodeConfig.DelayedSequencer.Enable = true

	err = builder.nodeConfig.Validate()
	RequireErr(t, err, expectedErr)

	builder.nodeConfig.DelayedSequencer.Enable = false
	builder.nodeConfig.SeqCoordinator.Enable = true

	err = builder.nodeConfig.Validate()
	RequireErr(t, err, expectedErr)

}
