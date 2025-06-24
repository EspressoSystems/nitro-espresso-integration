package arbtest

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
)

func createL1AndL2NodeForTimeboost(
	ctx context.Context,
	t *testing.T,
	delayedSequencer bool,
	blobsEnabled bool,
) (*NodeBuilder, func()) {
	builder := NewNodeBuilder(ctx).DefaultConfig(t, true)
	builder.l1StackConfig.HTTPPort = 8545
	builder.l1StackConfig.WSPort = 8546
	builder.l1StackConfig.HTTPHost = "0.0.0.0"
	builder.l1StackConfig.HTTPVirtualHosts = []string{"*"}
	builder.l1StackConfig.WSHost = "0.0.0.0"
	builder.l1StackConfig.DataDir = t.TempDir()
	builder.l1StackConfig.WSModules = append(builder.l1StackConfig.WSModules, "eth")
	builder.l2StackConfig.HTTPPort = 8945
	builder.l2StackConfig.HTTPHost = "0.0.0.0"
	builder.l2StackConfig.IPCPath = tmpPath(t, "test.ipc")
	builder.useL1StackConfig = true

	// poster config
	builder.nodeConfig.BatchPoster.Enable = false

	// validator config
	builder.nodeConfig.BlockValidator.Enable = true
	builder.nodeConfig.BlockValidator.ValidationPoll = 2 * time.Second
	builder.nodeConfig.BlockValidator.ValidationServer.URL = fmt.Sprintf("ws://127.0.0.1:%d", arbValidationPort)
	builder.nodeConfig.DelayedSequencer.Enable = delayedSequencer
	builder.nodeConfig.DelayedSequencer.FinalizeDistance = 1

	// sequencer config
	builder.nodeConfig.Sequencer = false
	builder.nodeConfig.ParentChainReader.Enable = true // This flag is necessary to enable sequencing transactions with espresso behavior
	builder.nodeConfig.ParentChainReader.UseFinalityData = true
	builder.nodeConfig.Dangerous.NoSequencerCoordinator = true
	builder.execConfig.Sequencer.Enable = false
	builder.execConfig.Caching.StateScheme = "hash"
	builder.execConfig.Caching.Archive = true

	// Enable timeboost sequencer
	builder.nodeConfig.TimeboostSequencer.Enable = true
	builder.nodeConfig.TimeboostSequencer.BlockRetryDuration = time.Second
	builder.nodeConfig.TimeboostSequencer.MaxTxDataSize = 8000
	builder.nodeConfig.TimeboostSequencer.NonceCacheSize = 1024
	builder.nodeConfig.TimeboostSequencer.MaxRevertGasReject = 0
	builder.nodeConfig.TimeboostSequencer.ParentChainFinalizationTime = 20 * time.Minute
	builder.nodeConfig.TimeboostSequencer.MaxAcceptableTimestampDelta = time.Hour
	builder.nodeConfig.TimeboostSequencer.EnableProfiling = false

	cleanup := builder.Build(t)

	mnemonic := "indoor dish desk flag debris potato excuse depart ticket judge file exit"
	err := builder.L1Info.GenerateAccountWithMnemonic("CommitmentTask", mnemonic, 5)
	Require(t, err)
	builder.L1.TransferBalance(t, "Faucet", "CommitmentTask", new(big.Int).Mul(big.NewInt(9e18), big.NewInt(1000)), builder.L1Info)

	return builder, cleanup
}

func TestEspressoTimeboostSequencer(t *testing.T) {
	t.Run("Run simple test to see if it builds the block", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		valNodeCleanup := createValidationNode(ctx, t, true)
		defer valNodeCleanup()
		// In future, we also need to create a version of
		// delayed sequencer for timeboost
		builder, cleanup := createL1AndL2NodeForTimeboost(ctx, t, true, false)
		defer cleanup()

		err := waitForL1Node(ctx)
		Require(t, err)

		var txs types.Transactions

		var users []string

		const numUsers = 100
		blockNumberBefore, err := builder.L2.Client.BlockNumber(ctx)
		Require(t, err)

		for num := 0; num < numUsers; num++ {
			userName := fmt.Sprintf("My_User_%d", num)
			builder.L2Info.GenerateAccount(userName)
			users = append(users, userName)
		}

		for _, userName := range users {
			tx := builder.L2Info.PrepareTx("Owner", userName, builder.L2Info.TransferGas, big.NewInt(2), nil)
			txs = append(txs, tx)
		}

		timeboostSequencer := builder.L2.ConsensusNode.TimeboostSequencer

		for _, tx := range txs {
			go func(ptx *types.Transaction) {
				err := timeboostSequencer.PublishTestTransaction(ctx, ptx, nil)
				Require(t, err)
			}(tx)
		}

		// Check that a block is created aftersometime
		time.Sleep(time.Second * 5)

		// Check that the database now has updated block
		blockNumberAfter, err := builder.L2.Client.BlockNumber(ctx)
		Require(t, err)

		// msgCntAfter should be 1 greater than msgCntBefore
		if blockNumberAfter-blockNumberBefore != 1 {
			t.Fatalf("expected msgCntAfter to be 1 greater than msgCntBefore, got: %d", blockNumberAfter-blockNumberBefore)
		}

		// Check that if that block contains all the tx hashes

		if blockNumberAfter > math.MaxInt64 {
			t.Fatalf("expected blockNumberAfter to be less than max int64, got: %d", blockNumberAfter)
		}
		block, err := builder.L1.Client.BlockByNumber(ctx, big.NewInt(int64(blockNumberAfter)))
		Require(t, err)
		for i, tx := range block.Transactions() {
			if tx.Hash() != txs[i].Hash() {
				t.Fatalf("expected tx hash to be in block, got: %s", tx.Hash().Hex())
			}
		}
	})

}
