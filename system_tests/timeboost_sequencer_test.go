package arbtest

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"google.golang.org/protobuf/proto"

	gethexec "github.com/offchainlabs/nitro/execution/gethexec/inclusion_list"
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

		var incls []*gethexec.InclusionList

		var users []string

		const numUsers = 10

		for num := 0; num < numUsers; num++ {
			userName := fmt.Sprintf("My_User_%d", num)
			builder.L2Info.GenerateAccount(userName)
			users = append(users, userName)
		}

		blockNumberBefore, err := builder.L2.Client.BlockNumber(ctx)
		Require(t, err)

		for i, userName := range users {
			tx := builder.L2Info.PrepareTx("Owner", userName, builder.L2Info.TransferGas, big.NewInt(2), nil)
			txBytes, err := tx.MarshalBinary()
			Require(t, err)
			if i < 0 {
				return
			}
			incl := &gethexec.InclusionList{
				Round:              uint64(i),
				ConsensusTimestamp: uint64(i),
				EncodedTxns: []*gethexec.Transaction{
					{
						EncodedTxn: txBytes,
						Address:    []byte{0x00},
						Timestamp:  1,
					},
				},
				DelayedMessagesRead: 0,
			}
			incls = append(incls, incl)
		}

		conn, err := net.Dial("tcp", "localhost:55000")
		if err != nil {
			t.Fatalf("Error connecting: %v", err)
			return
		}
		defer conn.Close()
		for _, incl := range incls {
			inclBytes, err := proto.Marshal(incl)
			Require(t, err)
			len := len(inclBytes)
			if len < 0 || len > math.MaxUint32 {
				t.Fatalf("Invalid len %d", len)
			}
			length := uint32(len)
			lengthBuf := make([]byte, 4)
			binary.BigEndian.PutUint32(lengthBuf, length)
			_, err = conn.Write(lengthBuf)
			Require(t, err)
			_, err = conn.Write(inclBytes)
			Require(t, err)

			buffer := make([]byte, 1)
			n, err := conn.Read(buffer)
			Require(t, err)
			if n != 1 {
				t.Fatalf("Expected to read 1 byte, read %d\n", n)
			}
			if buffer[0] != 0xc0 {
				t.Fatalf("Unexpected response byte: 0x%02x, expected 0xc0\n", buffer[0])
			}

			Require(t, err)
		}

		// Wait for sometime for the block to be produced
		time.Sleep(time.Second * 10)

		// Check that the database now has updated block
		blockNumberAfter, err := builder.L2.Client.BlockNumber(ctx)
		Require(t, err)

		// msgCntAfter should be 1 greater than msgCntBefore
		if blockNumberAfter-blockNumberBefore <= 0 {
			t.Fatalf("expected difference between blockNumberAfter and blockNumberBefore to be greater than 0, got: %d", blockNumberAfter-blockNumberBefore)
		}

		// Check that if that block contains all the tx hashes
		if blockNumberAfter > math.MaxInt64 {
			t.Fatalf("expected blockNumberAfter to be less than max int64, got: %d", blockNumberAfter)
		}

		// Get all the transactions from all the blocks after the blockNumberBefore
		var transactions []*types.Transaction
		for i := blockNumberBefore + 1; i <= blockNumberAfter; i++ {
			if i > math.MaxInt64 {
				t.Fatalf("expected blockNumberAfter to be less than max int64, got: %d", blockNumberAfter)
			}
			block, err := builder.L2.Client.BlockByNumber(ctx, big.NewInt(int64(i)))
			Require(t, err)
			blockTransactions := block.Transactions()
			transactionsWithoutStartBlock := blockTransactions[1:]
			transactions = append(transactions, transactionsWithoutStartBlock...)
		}

		for i, tx := range transactions {
			incl := incls[i]
			var expected types.Transaction
			err = expected.UnmarshalBinary(incl.EncodedTxns[0].EncodedTxn)
			Require(t, err)
			if tx.Hash() != expected.Hash() {
				t.Fatalf("txHash doesn't match, got %s, want %s", tx.Hash().Hex(), expected.Hash().Hex())
			}
		}
	})

}
