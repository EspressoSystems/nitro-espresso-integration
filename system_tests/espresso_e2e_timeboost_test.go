package arbtest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/agglayer/aggkit/test/contracts/erc1967proxy"
	"github.com/btcsuite/btcutil/base58"
	"github.com/prysmaticlabs/go-ssz"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/solgen/go/decentralizedtimeboostgen"
)

var timeBoostHealth = "/i/health"
var timeBoostSubmit = "/v1/submit/regular"
var timeboostUrls = []string{
	"http://localhost:8004", "http://localhost:8014",
}

func runDecentralizedTimeboost() func() {
	shutdown := func() {
		log.Warn("shutdown timeboost docker")
		p := exec.Command("docker", "compose", "-f", "docker-compose.timeboost.yml", "down", "--volumes")
		p.Dir = workingDir
		var stderr bytes.Buffer
		p.Stderr = &stderr
		if err := p.Run(); err != nil {
			log.Error("failed to run 'docker compose down`", "err", err, "str", stderr.String())
			panic(err)
		}
		time.Sleep(5 * time.Second)
	}
	shutdown()

	cmd := exec.Command("docker", "compose", "-f", "docker-compose.timeboost.yml", "up", "-d")
	cmd.Dir = workingDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Error("failed to run 'docker compose up`", "err", err, "str", stderr.String())
		panic(err)
	}

	return shutdown
}

func waitForTimeboostNodes(ctx context.Context) error {
	for _, timeboostUrl := range timeboostUrls {
		if err := waitForWith(ctx, 1*time.Minute, 1*time.Second, func() bool {
			resp, err := http.Get(timeboostUrl + timeBoostHealth)
			if err != nil {
				log.Warn("retry to check the timeboost health", "err", err)
				return false
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				log.Warn("retry to check the timeboost health", "code", resp.StatusCode)
				return false
			}
			return true
		}); err != nil {
			return err
		}
	}

	return nil
}

type Bundle struct {
	Chain     int      `json:"chain"`
	Epoch     uint64   `json:"epoch"`
	Data      string   `json:"data"`
	Encrypted bool     `json:"encrypted"`
	Hash      [32]byte `json:"hash"`
}

func NewBundle(chain int, epoch uint64, data []byte, hash common.Hash) Bundle {
	return Bundle{
		Chain:     chain,
		Epoch:     epoch,
		Data:      "0x" + hex.EncodeToString(data),
		Encrypted: false,
		Hash:      hash,
	}
}

func createAndSendBundleToTimeboost(t *testing.T, builder *NodeBuilder, users []string) []*types.Transaction {
	var expectedTxs []*types.Transaction
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	// Various test cases at a given index in the user loop
	twoTxnsInBundleIdx := 8            // Send two txns in a bundle
	sendTxnToOneTimeboostNodeIdx := 10 // Only send the bundle to one timeboost node
	for i, userName := range users {
		tx := builder.L2Info.PrepareTx("Owner", userName, builder.L2Info.TransferGas, big.NewInt(7000000000000001), nil)
		expectedTxs = append(expectedTxs, tx)
		txBytes, err := tx.MarshalBinary()
		Require(t, err)
		var encoded []byte
		if i == twoTxnsInBundleIdx {
			// Send 2 transactions in a bundle
			tx = builder.L2Info.PrepareTx("Owner", userName, builder.L2Info.TransferGas, big.NewInt(2), nil)
			expectedTxs = append(expectedTxs, tx)
			txBytes2, err := tx.MarshalBinary()
			Require(t, err)
			encoded, err = ssz.Marshal([][]byte{txBytes, txBytes2})
			Require(t, err)
		} else {
			encoded, err = ssz.Marshal([][]byte{txBytes})
			Require(t, err)
		}

		current := time.Now().Unix()
		if current < 0 {
			t.Fatalf("Invalid time %d", current)
		}
		epoch := uint64(current)
		bundle := NewBundle(0, epoch, encoded, tx.Hash())
		jsonData, err := json.MarshalIndent(bundle, "", "  ")
		Require(t, err)

		// Send to both nodes
		for _, timeboostUrl := range timeboostUrls {
			url := timeboostUrl + timeBoostSubmit
			req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
			Require(t, err)

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			_, err = client.Do(req)
			Require(t, err)
			if i == sendTxnToOneTimeboostNodeIdx {
				// Only send to one node
				// This should still be fine and include the transaction
				continue
			}
		}
		time.Sleep(1 * time.Second)
	}
	return expectedTxs
}

func setMockTimeboostKeyManagerContract(t *testing.T, ctx context.Context, l1Client *ethclient.Client, parentChainTransactionOpts bind.TransactOpts) common.Address {
	addr, tx, _, err := decentralizedtimeboostgen.DeployMockKeyManager(&parentChainTransactionOpts, l1Client)
	Require(t, err)
	_, err = bind.WaitMined(ctx, l1Client, tx)
	Require(t, err)
	return addr
}

func setupTimeboostKeyManagerContract(t *testing.T, ctx context.Context, l1Client *ethclient.Client, parentChainTransactionOpts bind.TransactOpts) common.Address {
	address, tx, _, err := decentralizedtimeboostgen.DeployKeyManager(&parentChainTransactionOpts, l1Client)
	if err != nil {
		t.Fatalf("error deploying key manager contract: %v", err)
	}
	_, err = bind.WaitMined(ctx, l1Client, tx)
	Require(t, err)
	decoded1 := base58.Decode("eiwaGN1NNaQdbnR9FsjKzUeLghQZsTLPjiL4RcQgfLoX")
	uncompressed1, err := crypto.DecompressPubkey(decoded1)
	Require(t, err)
	decoded2 := base58.Decode("vGKKAxVNfkSCdn8qh36nXdSZqyhPq644sQBoeZtcEUCR")
	uncompressed2, err := crypto.DecompressPubkey(decoded2)
	Require(t, err)

	members := []decentralizedtimeboostgen.KeyManagerCommitteeMember{
		{
			SigKey:             base58.Decode("eiwaGN1NNaQdbnR9FsjKzUeLghQZsTLPjiL4RcQgfLoX"),
			DhKey:              base58.Decode("AZrLbV37HAGhBWh49JHzup6Wfpu2AAGWGJJnxCDJibiY"),
			DkgKey:             base58.Decode("7PdmfTS45d2hTXB8NcrTmvDwUVBimpYBbrBaGnu3i5Ne65krVfUpbe7bYRHS3AEg7H"),
			NetworkAddress:     "node0:8000",
			BatchPosterAddress: "http://localhost:8945",
			SigKeyAddress:      crypto.PubkeyToAddress(*uncompressed1),
		},
		{
			SigKey:             base58.Decode("vGKKAxVNfkSCdn8qh36nXdSZqyhPq644sQBoeZtcEUCR"),
			DhKey:              base58.Decode("FHTJAk6oyt3jefEp1ZrPEn2MkqRt2LibEFd57AnEUZdb"),
			DkgKey:             base58.Decode("7p1BtEz7WnFMt6Hr28X3Rngqza6i8hRoswhzZRFd6GzgkspLKHBfDocHP8DwzXiNiZ"),
			NetworkAddress:     "node1:8010",
			BatchPosterAddress: "http://localhost:8947",
			SigKeyAddress:      crypto.PubkeyToAddress(*uncompressed2),
		},
	}

	keyManagerABI, err := abi.JSON(strings.NewReader(decentralizedtimeboostgen.KeyManagerABI))
	Require(t, err)
	initData, err := keyManagerABI.Pack("initialize", parentChainTransactionOpts.From)
	Require(t, err)

	proxyAddr, tx, _, err := erc1967proxy.DeployErc1967proxy(&parentChainTransactionOpts, l1Client, address, initData)
	Require(t, err)
	receipt, err := bind.WaitMined(ctx, l1Client, tx)
	Require(t, err)
	if receipt.Status == 0 {
		t.Fatal("Proxy deployment failed")
	}

	proxyContract, err := decentralizedtimeboostgen.NewKeyManager(proxyAddr, l1Client)
	if err != nil {
		t.Fatalf("Failed to bind proxy as KeyManager: %v", err)
	}

	manager, err := proxyContract.Manager(&bind.CallOpts{})
	if err != nil {
		t.Fatalf("Failed to call manager(): %v", err)
	}
	if manager != parentChainTransactionOpts.From {
		t.Fatalf("Manager not set correctly: got %s, want %s", manager.Hex(), parentChainTransactionOpts.From.Hex())
	}

	timestamp := time.Now().Unix() - 1000
	if timestamp < 0 {
		t.Fatalf("timestamp cannot be negative")
	}
	tx, err = proxyContract.SetNextCommittee(&parentChainTransactionOpts, uint64(timestamp), members)
	Require(t, err)
	receipt, err = bind.WaitMined(ctx, l1Client, tx)
	Require(t, err)
	if receipt.Status == 0 {
		t.Fatal("failed to set next committee", "err", err)
	}

	id, err := proxyContract.CurrentCommitteeId(&bind.CallOpts{})
	Require(t, err)
	_, err = proxyContract.GetCommitteeById(&bind.CallOpts{}, id)
	Require(t, err)
	return proxyAddr
}

func TestEspressoTimeboostSequencerE2E(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	valNodeCleanup := createValidationNode(ctx, t, true)
	defer valNodeCleanup()

	builder, _ := createL1AndL2NodeForTimeboost(ctx, t, true, true, "3hzb3bRzn3dXSV1iEVE6mU4BF2aS725s8AboRxLwULPp", nil, false)
	builder2, _ := createL1AndL2NodeForTimeboost(ctx, t, true, true, "FWJzNGvEjFS3h1N1sSMkcvvroWwjT5LQuGkGHu9JMAYs", builder, false)

	err := waitForL1Node(ctx)
	Require(t, err)

	shutdown := runDecentralizedTimeboost()
	defer shutdown()

	err = waitForEspressoNode(ctx)
	Require(t, err)

	err = waitForTimeboostNodes(ctx)
	Require(t, err)

	var users []string
	const numUsers = 15

	blockNumberBefore, err := builder.L2.Client.BlockNumber(ctx)
	Require(t, err)

	log.Info("addr", "ibox addr", builder.addresses.Inbox, "seq", builder.addresses.SequencerInbox)

	for num := 0; num < numUsers; num++ {
		userName := fmt.Sprintf("My_User_%d", num)
		builder.L2Info.GenerateAccount(userName)
		users = append(users, userName)
	}

	expectedTxs := createAndSendBundleToTimeboost(t, builder, users)
	// account 2 transactions in a bundle
	if len(expectedTxs) != numUsers+1 {
		t.Fatalf("expected transactions should be num users + 1. num users %d, expected len %d", numUsers, len(expectedTxs))
	}

	// Send some delayed messages, any user should be able to do so, not just the owner
	delayedTx := builder.L2Info.PrepareTx(users[0], users[1], 3e7, big.NewInt(1), nil)
	builder.L1.SendWaitTestTransactions(t, []*types.Transaction{
		WrapL2ForDelayed(t, delayedTx, builder.L1Info, "Faucet", 100000),
	})
	delayedTx2 := builder.L2Info.PrepareTx(users[1], users[2], 3e7, big.NewInt(1), nil)
	builder.L1.SendWaitTestTransactions(t, []*types.Transaction{
		WrapL2ForDelayed(t, delayedTx2, builder.L1Info, "Faucet", 100000),
	})
	// User has no funds so TX should fail
	builder.L2Info.GenerateAccount("luke")
	invalidTx := builder.L2Info.PrepareTx("luke", users[2], 3e7, big.NewInt(1), nil)
	builder.L1.SendWaitTestTransactions(t, []*types.Transaction{
		WrapL2ForDelayed(t, invalidTx, builder.L1Info, "Faucet", 100000),
	})
	// Send another transaction
	expectedTxs = append(expectedTxs, createAndSendBundleToTimeboost(t, builder, []string{users[10]})...)
	// We expect delayed messages blocks to be built last
	expectedTxs = append(expectedTxs, delayedTx)
	expectedTxs = append(expectedTxs, delayedTx2)

	// Wait for blocks and batch
	time.Sleep(time.Second * 40)

	blockNumberAfter, err := builder.L2.Client.BlockNumber(ctx)
	Require(t, err)

	// msgCntAfter should be greater than msgCntBefore
	if blockNumberAfter-blockNumberBefore <= 0 {
		t.Fatalf("expected difference between blockNumberAfter and blockNumberBefore to be greater than 0, got: %d", blockNumberAfter-blockNumberBefore)
	}

	// Insanity check
	if blockNumberAfter > math.MaxInt64 {
		t.Fatalf("expected blockNumberAfter to be less than max int64, got: %d", blockNumberAfter)
	}

	// Verify blocks are the same from both sequencers
	var transactions []*types.Transaction
	var transactions2 []*types.Transaction
	for i := blockNumberBefore + 1; i <= blockNumberAfter; i++ {
		if i > math.MaxInt64 {
			t.Fatalf("expected blockNumberAfter to be less than max int64, got: %d", blockNumberAfter)
		}
		block, err := builder.L2.Client.BlockByNumber(ctx, big.NewInt(int64(i)))
		Require(t, err)
		blockTransactions := block.Transactions()
		transactionsWithoutStartBlock := blockTransactions[1:]
		transactions = append(transactions, transactionsWithoutStartBlock...)
		block, err = builder2.L2.Client.BlockByNumber(ctx, big.NewInt(int64(i)))
		Require(t, err)
		blockTransactions = block.Transactions()
		transactionsWithoutStartBlock = blockTransactions[1:]
		transactions2 = append(transactions2, transactionsWithoutStartBlock...)
	}

	// Verify both sequencers blocks with expected
	for i, tx := range expectedTxs {
		expected := transactions[i]
		if tx.Hash() != expected.Hash() {
			t.Fatalf("txHash doesn't match, got %s, want %s.", tx.Hash().Hex(), expected.Hash().Hex())
		}
		expected = transactions2[i]
		if tx.Hash() != expected.Hash() {
			t.Fatalf("txHash doesn't match, got %s, want %s.", tx.Hash().Hex(), expected.Hash().Hex())
		}
	}

	err = waitForWith(ctx, 1*time.Minute, 5*time.Second, func() bool {
		// Check the sequencer inbox contract
		sequencerInbox, err := bridgegen.NewSequencerInbox(builder.L1Info.GetAddress("SequencerInbox"), builder.L1.Client)
		Require(t, err)

		batchCount, err := sequencerInbox.BatchCount(&bind.CallOpts{Context: ctx})
		Require(t, err)
		return batchCount.Uint64() > 1
	})
	Require(t, err)
	builder.L2.cleanup()
	builder.L1.cleanup()
	builder2.L2.cleanup()
}

func TestEspressoTimeboostSequencerE2EWithBlobs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder, _ := createL1AndL2NodeForTimeboost(ctx, t, true, true, "3hzb3bRzn3dXSV1iEVE6mU4BF2aS725s8AboRxLwULPp", nil, true)
	builder2, _ := createL1AndL2NodeForTimeboost(ctx, t, true, true, "FWJzNGvEjFS3h1N1sSMkcvvroWwjT5LQuGkGHu9JMAYs", builder, true)

	err := waitForL1Node(ctx)
	Require(t, err)

	shutdown := runDecentralizedTimeboost()
	defer shutdown()

	err = waitForEspressoNode(ctx)
	Require(t, err)

	err = waitForTimeboostNodes(ctx)
	Require(t, err)

	var users []string
	const numUsers = 15

	blockNumberBefore, err := builder.L2.Client.BlockNumber(ctx)
	Require(t, err)

	for num := 0; num < numUsers; num++ {
		userName := fmt.Sprintf("My_User_%d", num)
		builder.L2Info.GenerateAccount(userName)
		users = append(users, userName)
	}

	expectedTxs := createAndSendBundleToTimeboost(t, builder, users)
	// account 2 transactions in a bundle
	if len(expectedTxs) != numUsers+1 {
		t.Fatalf("expected transactions should be num users + 1. num users %d, expected len %d", numUsers, len(expectedTxs))
	}

	// Send some delayed messages, any user should be able to do so, not just the owner
	delayedTx := builder.L2Info.PrepareTx(users[0], users[1], 3e7, big.NewInt(1), nil)
	builder.L1.SendWaitTestTransactions(t, []*types.Transaction{
		WrapL2ForDelayed(t, delayedTx, builder.L1Info, "Faucet", 100000),
	})
	delayedTx2 := builder.L2Info.PrepareTx(users[1], users[2], 3e7, big.NewInt(1), nil)
	builder.L1.SendWaitTestTransactions(t, []*types.Transaction{
		WrapL2ForDelayed(t, delayedTx2, builder.L1Info, "Faucet", 100000),
	})
	// User has no funds so TX should fail
	builder.L2Info.GenerateAccount("luke")
	invalidTx := builder.L2Info.PrepareTx("luke", users[2], 3e7, big.NewInt(1), nil)
	builder.L1.SendWaitTestTransactions(t, []*types.Transaction{
		WrapL2ForDelayed(t, invalidTx, builder.L1Info, "Faucet", 100000),
	})
	// Send another transaction
	expectedTxs = append(expectedTxs, createAndSendBundleToTimeboost(t, builder, []string{users[10]})...)
	// We expect delayed messages blocks to be built last
	expectedTxs = append(expectedTxs, delayedTx)
	expectedTxs = append(expectedTxs, delayedTx2)

	// Wait for blocks and batch
	time.Sleep(time.Second * 40)

	blockNumberAfter, err := builder.L2.Client.BlockNumber(ctx)
	Require(t, err)

	// msgCntAfter should be greater than msgCntBefore
	if blockNumberAfter-blockNumberBefore <= 0 {
		t.Fatalf("expected difference between blockNumberAfter and blockNumberBefore to be greater than 0, got: %d", blockNumberAfter-blockNumberBefore)
	}

	// Insanity check
	if blockNumberAfter > math.MaxInt64 {
		t.Fatalf("expected blockNumberAfter to be less than max int64, got: %d", blockNumberAfter)
	}

	// Verify blocks are the same from both sequencers
	var transactions []*types.Transaction
	var transactions2 []*types.Transaction
	for i := blockNumberBefore + 1; i <= blockNumberAfter; i++ {
		if i > math.MaxInt64 {
			t.Fatalf("expected blockNumberAfter to be less than max int64, got: %d", blockNumberAfter)
		}
		block, err := builder.L2.Client.BlockByNumber(ctx, big.NewInt(int64(i)))
		Require(t, err)
		blockTransactions := block.Transactions()
		transactionsWithoutStartBlock := blockTransactions[1:]
		transactions = append(transactions, transactionsWithoutStartBlock...)
		block, err = builder2.L2.Client.BlockByNumber(ctx, big.NewInt(int64(i)))
		Require(t, err)
		blockTransactions = block.Transactions()
		transactionsWithoutStartBlock = blockTransactions[1:]
		transactions2 = append(transactions2, transactionsWithoutStartBlock...)
	}

	// Verify both sequencers blocks with expected
	for i, tx := range expectedTxs {
		expected := transactions[i]
		if tx.Hash() != expected.Hash() {
			t.Fatalf("txHash doesn't match, got %s, want %s.", tx.Hash().Hex(), expected.Hash().Hex())
		}
		expected = transactions2[i]
		if tx.Hash() != expected.Hash() {
			t.Fatalf("txHash doesn't match, got %s, want %s.", tx.Hash().Hex(), expected.Hash().Hex())
		}
	}

	err = waitForWith(ctx, 1*time.Minute, 5*time.Second, func() bool {
		// Check the sequencer inbox contract
		sequencerInbox, err := bridgegen.NewSequencerInbox(builder.L1Info.GetAddress("SequencerInbox"), builder.L1.Client)
		Require(t, err)

		batchCount, err := sequencerInbox.BatchCount(&bind.CallOpts{Context: ctx})
		Require(t, err)
		return batchCount.Uint64() > 1
	})
	Require(t, err)
	builder.L2.cleanup()
	builder.L1.cleanup()
	builder2.L2.cleanup()
}
