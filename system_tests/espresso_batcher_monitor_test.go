package arbtest

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"

	"github.com/offchainlabs/bold/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/arbnode"
)

func TestEspressoBatcherMonitor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder, cleanup := createL1AndL2Node(ctx, t, true, false)
	defer cleanup()

	err := waitForL1Node(ctx)
	Require(t, err)

	shutdown := runEspresso()
	defer shutdown()

	seqInboxAddr := builder.addresses.SequencerInbox

	monitor := arbnode.NewBatcherAddrMonitor(
		[]common.Address{},
		rawdb.NewMemoryDatabase(),
		builder.L2.ConsensusNode.L1Reader,
		seqInboxAddr,
		builder.L2.ConsensusNode.DeployInfo.DeployedAt,
	)
	err = monitor.Start(ctx)
	Require(t, err)

	abi, err := bridgegen.SequencerInboxMetaData.GetAbi()
	Require(t, err)
	batchPosterAddr := builder.L2Info.GetAddress("Faucet")
	data, err := abi.Pack("setIsBatchPoster", batchPosterAddr, true)
	Require(t, err)
	tx := builder.L1Info.PrepareTxTo("RollupOwner", &seqInboxAddr, 100000, big.NewInt(0), data)
	err = builder.L1.Client.SendTransaction(ctx, tx)
	Require(t, err)
	receipt, err := EnsureTxSucceededWithTimeout(ctx, builder.L1.Client, tx, time.Second*10)
	Require(t, err)
	l1Height := receipt.BlockNumber.Uint64() + 1

	err = waitFor(ctx, func() bool {
		return monitor.GetConfirmedParentHeight() >= l1Height
	})
	Require(t, err)

	validAddresses := monitor.GetValidAddresses(l1Height)
	if len(validAddresses) != 1 {
		t.Fatal("expected 1 valid address, got", validAddresses)
	}
	if validAddresses[0] != batchPosterAddr {
		t.Fatal("expected valid address to be", batchPosterAddr, "got", validAddresses[0])
	}

	newAddr := common.Address{}
	data2, err := abi.Pack("setIsBatchPoster", newAddr, true)
	Require(t, err)
	tx2 := builder.L1Info.PrepareTxTo("RollupOwner", &seqInboxAddr, 100000, big.NewInt(0), data2)
	err = builder.L1.Client.SendTransaction(ctx, tx2)
	Require(t, err)
	receipt2, err := EnsureTxSucceededWithTimeout(ctx, builder.L1.Client, tx2, time.Second*10)
	Require(t, err)

	l1Height2 := receipt2.BlockNumber.Uint64()

	err = waitFor(ctx, func() bool {
		return monitor.GetConfirmedParentHeight() > l1Height2
	})
	Require(t, err)

	validAddresses2 := monitor.GetValidAddresses(l1Height2)
	if len(validAddresses2) != 2 {
		t.Fatal("expected 2 valid addresses, got", validAddresses2)
	}
	if validAddresses2[1] != newAddr {
		t.Fatal("expected valid address to be", newAddr, "got", validAddresses2[1])
	}

}
