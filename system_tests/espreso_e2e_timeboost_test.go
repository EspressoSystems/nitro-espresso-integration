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
	"testing"
	"time"

	"github.com/prysmaticlabs/go-ssz"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

var timeBoostHealth = "/healthz"
var timeBoostSubmit = "/submit-regular"
var timeboostUrls = []string{
	"http://localhost:8800/v0", "http://localhost:8801/v0",
}

func runDecentralizedTimeboost() func() {
	shutdown := func() {
		log.Warn("shutdown timeboost docker")
		p := exec.Command("docker", "compose", "-f", "docker-compose.timeboost.yml", "down", "--volumes")
		p.Dir = workingDir
		var stdout, stderr bytes.Buffer
		p.Stdout = &stdout
		p.Stderr = &stderr
		if err := p.Run(); err != nil {
			log.Error("failed to run 'docker compose down", "err", err, "str", stderr.String())
			panic(err)
		}
		time.Sleep(5 * time.Second)
	}
	shutdown()

	cmd := exec.Command("docker", "compose", "-f", "docker-compose.timeboost.yml", "up", "-d")
	cmd.Dir = workingDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Error("failed to run 'docker compose up", "err", err, "str", stderr.String())
		panic(err)
	}

	return shutdown
}

func waitForTimeboostNodes(ctx context.Context) error {
	for _, timeboostUrl := range timeboostUrls {
		if err := waitForWith(ctx, 3*time.Minute, 1*time.Second, func() bool {
			resp, err := http.Get(timeboostUrl + timeBoostHealth)
			if err != nil {
				log.Warn("retry to check the timeboost health", "err", err)
				return false
			}
			if resp.StatusCode != http.StatusOK {
				log.Warn("retry to check the timeboost health", "code", resp.StatusCode)
				return false
			}
			defer resp.Body.Close()
			return true
		}); err != nil {
			return err
		}
	}

	return nil
}

type Bundle struct {
	Chain int      `json:"chain"`
	Epoch uint64   `json:"epoch"`
	Data  string   `json:"data"`
	Kid   *uint64  `json:"kid"`
	Hash  [32]byte `json:"hash"`
}

func NewBundle(chain int, epoch uint64, data []byte, hash common.Hash) Bundle {
	return Bundle{
		Chain: chain,
		Epoch: epoch,
		Data:  "0x" + hex.EncodeToString(data),
		Kid:   nil,
		Hash:  hash,
	}
}

func TestEspressoTimeboostSequencerE2E(t *testing.T) {
	t.Run("Run e2e test with timeboost, by sending transactions to spun up timeboost images", func(t *testing.T) {
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

		shutdown := runDecentralizedTimeboost()
		defer shutdown()

		err = waitForTimeboostNodes(ctx)
		Require(t, err)

		var users []string
		const numUsers = 10

		blockNumberBefore, err := builder.L2.Client.BlockNumber(ctx)
		Require(t, err)

		for num := 0; num < numUsers; num++ {
			userName := fmt.Sprintf("My_User_%d", num)
			builder.L2Info.GenerateAccount(userName)
			users = append(users, userName)
		}

		var expectedTxs []*types.Transaction
		for _, userName := range users {
			tx := builder.L2Info.PrepareTx("Owner", userName, builder.L2Info.TransferGas, big.NewInt(2), nil)
			expectedTxs = append(expectedTxs, tx)
			txBytes, err := tx.MarshalBinary()
			if err != nil {
				panic(fmt.Errorf("failed to encode transaction: %w", err))
			}
			encoded, err := ssz.Marshal([][]byte{txBytes})
			if err != nil {
				panic(fmt.Errorf("failed to SSZ encode: %w", err))
			}

			current := time.Now().Unix()
			if current < 0 {
				t.Fatalf("Invalid time %d", current)
			}
			epoch := uint64(current)

			bundle := NewBundle(0, epoch, encoded, tx.Hash())
			log.Info("sending bundle", "bundle", bundle)

			jsonData, err := json.MarshalIndent(bundle, "", "  ")
			Require(t, err)

			client := &http.Client{
				Timeout: 5 * time.Second,
			}
			for _, timeboostUrl := range timeboostUrls {
				url := timeboostUrl + timeBoostSubmit
				req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
				Require(t, err)

				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json")
				_, err = client.Do(req)
				Require(t, err)
			}
			time.Sleep(3 * time.Second)
		}
		// Wait for sometime for the blocks to be produced
		time.Sleep(time.Second * 10)

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

		if len(transactions) != len(expectedTxs) {
			t.Fatalf("expected transcations and block transactions to match. got %d expected txns, got %d block transactions", len(expectedTxs), len(transactions))
		}

		for i, tx := range transactions {
			expected := expectedTxs[i]
			if tx.Hash() != expected.Hash() {
				t.Fatalf("txHash doesn't match, got %s, want %s.", tx.Hash().Hex(), expected.Hash().Hex())
			}
		}

		time.Sleep(30 * time.Second)
	})

}
