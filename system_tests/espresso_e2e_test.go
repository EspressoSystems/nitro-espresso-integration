package arbtest

import (
	"context"
	"fmt"
	"math/big"
	"os/exec"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbutil"
)

var workingDir = "./espresso-e2e"

func runEspresso(t *testing.T, ctx context.Context) func() {
	shutdown := func() {
		p := exec.Command("docker-compose", "down")
		p.Dir = workingDir
		err := p.Run()
		if err != nil {
			panic(err)
		}
	}

	shutdown()
	invocation := []string{"up", "-d"}
	nodes := []string{
		"orchestrator",
		"da-server",
		"consensus-server",
		"espresso-sequencer0",
		"espresso-sequencer1",
		"commitment-task",
	}
	invocation = append(invocation, nodes...)
	procees := exec.Command("docker-compose", invocation...)
	procees.Dir = workingDir

	go func() {
		if err := procees.Run(); err != nil {
			log.Error(err.Error())
			panic(err)
		}
	}()
	return shutdown
}

func createL1Node(ctx context.Context, t *testing.T) (*TestClient, func()) {
	builder := NewNodeBuilder(ctx).DefaultConfig(t, true)
	builder.l1StackConfig.HTTPPort = 8545
	builder.l1StackConfig.WSPort = 8546
	builder.l1StackConfig.HTTPHost = "127.0.0.1"
	builder.l1StackConfig.HTTPVirtualHosts = []string{"*"}
	builder.l1StackConfig.WSHost = "127.0.0.1"
	builder.l1StackConfig.DataDir = t.TempDir()
	builder.l1StackConfig.WSModules = append(builder.l1StackConfig.WSModules, "eth")

	builder.nodeConfig.Feed.Input.URL = []string{fmt.Sprintf("ws://127.0.0.1:%d", broadcastPort)}
	builder.nodeConfig.BatchPoster.Enable = true
	builder.nodeConfig.BlockValidator.Enable = true
	builder.nodeConfig.BlockValidator.ValidationServer.URL = fmt.Sprintf("ws://127.0.0.1:%d", validationPort)
	cleanup := builder.Build(t)

	mnemonic := "indoor dish desk flag debris potato excuse depart ticket judge file exit"
	err := builder.L1Info.GenerateAccountWithMnemonic("CommitmentTask", mnemonic, 5)
	if err != nil {
		panic(err)
	}
	builder.L1.TransferBalance(t, "Faucet", "CommitmentTask", big.NewInt(9e18), builder.L1Info)

	return builder.L2, cleanup
}

func TestEspressoE2E(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cleanValNode := createValidationNode(ctx, t)
	defer cleanValNode()

	node, cleanup := createL1Node(ctx, t)
	defer cleanup()

	err := waitFor(t, ctx, func() bool {
		if e := exec.Command(
			"curl",
			"-X",
			"POST",
			"-H",
			"Content-Type: application/json",
			"-d",
			"{'jsonrpc':'2.0','id':45678,'method':'eth_chainId','params':[]}",
			"http://localhost:8545",
		).Run(); e != nil {
			return false
		}
		return true
	})
	Require(t, err)

	l2Node, l2Info, cleanL2Node := createL2Node(ctx, t, "http://127.0.0.1:50000")
	defer cleanL2Node()
	time.Sleep(5 * time.Second)

	// _, cleanup := createValidatorAndPosterNode(ctx, t, false)
	// defer cleanup()

	cleanEspresso := runEspresso(t, ctx)
	defer cleanEspresso()

	// wait for the commitment task
	// err = waitFor(t, ctx, func() bool {
	// 	out, err := exec.Command("curl", "http://127.0.0.1:60000/api/hotshot_contract").Output()
	// 	if err != nil {
	// 		return false
	// 	}
	// 	log.Info(fmt.Sprintf("hotshot address: %s", string(out)))
	// 	return len(out) > 0
	// })
	// Require(t, err)

	// Wait for the initial message
	expected := arbutil.MessageIndex(1)
	err = waitFor(t, ctx, func() bool {
		msgCnt, err := l2Node.ConsensusNode.TxStreamer.GetMessageCount()
		if err != nil {
			panic(err)
		}

		validatedCnt := node.ConsensusNode.BlockValidator.Validated(t)
		return msgCnt >= expected && validatedCnt >= expected
	})
	Require(t, err)

	l2Info.GenerateAccount("User")
	addr := l2Info.GetAddress("User")
	balance := l2Node.GetBalance(t, addr)
	if balance.Cmp(big.NewInt(0)) > 0 {
		Fatal(t, "empty account")
	}

	// amount := big.NewInt(1e16)
	// tx := l2Info.PrepareTx("Faucet", "User", 3e7, amount, nil)
	// err = l2Node.Client.SendTransaction(ctx, tx)
	// Require(t, err)

	// err = waitFor(t, ctx, func() bool {
	// 	b := l2Node.GetBalance(t, addr)
	// 	return b.Cmp(amount) == 0
	// })
	// Require(t, err)

	err = waitFor(t, ctx, func() bool {
		validatedCnt := node.ConsensusNode.BlockValidator.Validated(t)
		msgCnt, _ := node.ConsensusNode.TxStreamer.GetMessageCount()
		return validatedCnt == msgCnt+10
	})
	Require(t, err)
}
