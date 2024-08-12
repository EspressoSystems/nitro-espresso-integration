package arbtest

import (
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/offchainlabs/nitro/solgen/go/challengegen"
	"github.com/offchainlabs/nitro/solgen/go/mocksgen"
	espressoOspGen "github.com/offchainlabs/nitro/solgen/go/ospgen"
	"github.com/offchainlabs/nitro/solgen/go/precompilesgen"
	"github.com/offchainlabs/nitro/solgen/go/upgrade_executorgen"
	"github.com/offchainlabs/nitro/util/arbmath"
	"github.com/offchainlabs/nitro/util/headerreader"
	"math/big"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbutil"
)

func BuildNonEspressoNetwork(ctx context.Context, t *testing.T) (*NodeBuilder, *TestClient, *BlockchainTestInfo, *SecondNodeParams, func()) {
	cleanValNode := createValidationNode(ctx, t, false)

	builder, cleanup := createL1ValidatorPosterNode(ctx, t, hotShotUrl, false)

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

	cleanEspresso := runEspresso(t, ctx)

	// wait for the builder
	err = waitForWith(t, ctx, 400*time.Second, 1*time.Second, func() bool {
		out, err := exec.Command("curl", "http://localhost:41000/availability/block/10", "-L").Output()
		if err != nil {
			log.Warn("retry to check the builder", "err", err)
			return false
		}
		return len(out) > 0
	})
	Require(t, err)

	l2Node, l2Info, secondNodeParams, cleanL2Node := createL2Node(ctx, t, hotShotUrl, builder, false)

	for k, v := range builder.L1Info.Accounts {
		log.Info("L2 Accounts", "Account pneumonic", k, "Entry", v, "Address", v.Address)
	}

	return builder, l2Node, l2Info, secondNodeParams, func() {
		cleanL2Node()
		cleanup()
		cleanValNode()
		cleanEspresso()
	}
}

func andTxSucceeded(ctx context.Context, l1Reader *headerreader.HeaderReader, tx *types.Transaction, err error) error {
	if err != nil {
		return fmt.Errorf("error submitting tx: %w", err)
	}
	_, err = l1Reader.WaitForTxApproval(ctx, tx)
	if err != nil {
		return fmt.Errorf("error executing tx: %w", err)
	}
	return nil
}

func DeployNewOspToL1(t *testing.T, ctx context.Context, l1client client, hotshot common.Address, auth *bind.TransactOpts) (common.Address, error) {
	arbSys, _ := precompilesgen.NewArbSys(types.ArbSysAddress, l1client)
	l1Reader, err := headerreader.New(ctx, l1client, func() *headerreader.Config { return &headerreader.TestConfig }, arbSys)
	Require(t, err)
	l1Reader.Start(ctx)
	defer l1Reader.StopAndWait()

	client := l1Reader.Client()

	osp0, tx, _, err := espressoOspGen.DeployOneStepProver0(auth, client)
	err = andTxSucceeded(ctx, l1Reader, tx, err)
	if err != nil {
		return common.Address{}, fmt.Errorf("osp0 deploy error: %w", err)
	}

	ospMem, tx, _, err := espressoOspGen.DeployOneStepProverMemory(auth, client)
	err = andTxSucceeded(ctx, l1Reader, tx, err)
	if err != nil {
		return common.Address{}, fmt.Errorf("ospMemory deploy error: %w", err)
	}

	ospMath, tx, _, err := espressoOspGen.DeployOneStepProverMath(auth, client)
	err = andTxSucceeded(ctx, l1Reader, tx, err)
	if err != nil {
		return common.Address{}, fmt.Errorf("ospMath deploy error: %w", err)
	}

	ospHostIo, tx, _, err := espressoOspGen.DeployOneStepProverHostIo(auth, client, hotshot)
	err = andTxSucceeded(ctx, l1Reader, tx, err)
	if err != nil {
		return common.Address{}, fmt.Errorf("ospHostIo deploy error: %w", err)
	}

	ospEntryAddr, tx, _, err := espressoOspGen.DeployOneStepProofEntry(auth, client, osp0, ospMem, ospMath, ospHostIo)
	err = andTxSucceeded(ctx, l1Reader, tx, err)
	if err != nil {
		return common.Address{}, fmt.Errorf("ospEntry deploy error: %w", err)
	}

	return ospEntryAddr, nil
}

// Name: errorIfAddressesEqual
// Desc: test utility function that will throw an error if 2 addresses are equal, useful for checking contract upgrades have succeeded.
func errorIfAddressesEqual(addr1 common.Address, addr2 common.Address) error {
	if addr1 == addr2 {
		return fmt.Errorf("address %s and address %s are the same", addr1, addr2)
	}
	return nil
}

// Name: errorIfAddressesUnequal
// Desc: test utility function that will throw an error if 2 addresses are unequal, useful for checking contract upgrades have succeeded.
func errorIfAddressesUnequal(addr1 common.Address, addr2 common.Address) error {
	if addr1 != addr2 {
		return fmt.Errorf("address %s and address %s are not the same", addr1, addr2)
	}
	return nil
}

// Check all the post upgrade assertions in one place even if it induces an obscenely long function signature.
func checkPostUpgradeAssertions(t *testing.T, proxyAdmin *mocksgen.ProxyAdminForBinding, callOpts *bind.CallOpts, challengeManagerAddress common.Address, challengeManagerImplAddr common.Address, challengeManager *challengegen.ChallengeManager, oldOspEntryAddress common.Address, newOspEntry common.Address) {

	updatedChallengeManagerImplAddr, err := proxyAdmin.GetProxyImplementation(callOpts, challengeManagerAddress)
	Require(t, err)

	//ensure the upgrade was actually a no-op
	err = errorIfAddressesUnequal(challengeManagerImplAddr, updatedChallengeManagerImplAddr)
	Require(t, err)

	updatedOspAddr, err := challengeManager.Osp(callOpts)
	Require(t, err)

	err = errorIfAddressesEqual(updatedOspAddr, oldOspEntryAddress)
	Require(t, err)
	log.Info("updatedOspAddr", "updatedOspAddr", updatedOspAddr, "OldOspEntryAddress", oldOspEntryAddress, "newOspEntry", newOspEntry)
	err = errorIfAddressesUnequal(updatedOspAddr, newOspEntry)
	Require(t, err)
}

func transferTxOnL2(
	t *testing.T,
	ctx context.Context,
	l2Node *TestClient,
	account string,
	l2Info *BlockchainTestInfo,
) error {
	transferAmount := big.NewInt(1e16)
	tx := l2Info.PrepareTx("Faucet", account, 3e7, transferAmount, nil)

	err := l2Node.Client.SendTransaction(ctx, tx)
	if err != nil {
		return err
	}

	addr := l2Info.GetAddress(account)

	return waitForWith(t, ctx, time.Second*300, time.Second*1, func() bool {
		balance := l2Node.GetBalance(t, addr)
		log.Info("waiting for balance", "account", account, "addr", addr, "balance", balance)
		return balance.Cmp(big.NewInt(100000)) >= 0
	})
}

func upgradeContracts(t *testing.T, l1Auth *bind.TransactOpts, l2Auth *bind.TransactOpts, ctx context.Context, builder *NodeBuilder) {
	l1Client := builder.L1.Client
	//get the current wasmmoduleroot.
	//locator, err := server_common.NewMachineLocator(builder.valnodeConfig.Wasm.RootPath)
	//Require(t, err)

	// wasmModuleRoot := locator.LatestWasmModuleRoot()
	wasmModuleRoot := common.HexToHash("newWasmModuleRoot")

	//deploy the new OSP contracts to the L1 and record their addresses/
	newOspEntry, err := DeployNewOspToL1(t, ctx, builder.L1.Client, common.HexToAddress(builder.nodeConfig.BlockValidator.LightClientAddress), l1Auth)
	//construct light client addr.
	Require(t, err)

	log.Info("Get challenge manager account")
	challengeManagerIndex := "ChallengeManager"
	challengeManagerAddress := builder.L1Info.GetAddress(challengeManagerIndex)

	challengeManager, err := challengegen.NewChallengeManager(challengeManagerAddress, builder.L1.Client)

	upgradeExecutorIndex := "UpgradeExecutor"
	upgradeExecutorAddress := builder.L1Info.GetAddress(upgradeExecutorIndex)

	oldOspEntryIndex := "OspEntry"
	oldOspEntryAddress := builder.L1Info.GetAddress(oldOspEntryIndex)

	log.Info("UpgradeOpts", "Opts:", l1Auth)
	callDataAbi, err := abi.JSON(strings.NewReader(challengegen.ChallengeManagerMetaData.ABI))
	Require(t, err)
	log.Info("Call Data:", "newOspEntry", newOspEntry, "wasmModuleRoot", wasmModuleRoot, "OldOspEntry", oldOspEntryAddress)
	callData, err := callDataAbi.Pack("postUpgradeInit", newOspEntry, wasmModuleRoot, oldOspEntryAddress)
	Require(t, err)

	//rollupAdmin = rollupgen.new

	proxyAdminSlot := common.BigToHash(arbmath.BigSub(crypto.Keccak256Hash([]byte("eip1967.proxy.admin")).Big(), common.Big1))
	proxyAdminBytes, err := builder.L1.Client.StorageAt(ctx, challengeManagerAddress, proxyAdminSlot, nil)
	Require(t, err)
	proxyAdminAddr := common.BytesToAddress(proxyAdminBytes)
	if proxyAdminAddr == (common.Address{}) {
		Fatal(t, "failed to get challenge manager proxy admin")
	}
	log.Info("ProxyAdmin", "addr", proxyAdminAddr)
	proxyAdminAbi, err := abi.JSON(strings.NewReader(mocksgen.ProxyAdminForBindingMetaData.ABI))
	Require(t, err)

	proxyAdmin, err := mocksgen.NewProxyAdminForBinding(proxyAdminAddr, l1Client)
	Require(t, err)

	callOpts := builder.L1Info.GetDefaultCallOpts("RollupOwner", ctx)

	challengeManagerImplAddr, err := proxyAdmin.GetProxyImplementation(callOpts, challengeManagerAddress)
	Require(t, err)
	proxyAdminCallData, err := proxyAdminAbi.Pack("upgradeAndCall", challengeManagerAddress, challengeManagerImplAddr, callData) // change 2nd chall addr to actual impl addr
	Require(t, err)

	upgradeExecutor, err := upgrade_executorgen.NewUpgradeExecutor(upgradeExecutorAddress, l1Client)

	Require(t, err)

	tx, err := upgradeExecutor.ExecuteCall(l1Auth, proxyAdminAddr, proxyAdminCallData)
	Require(t, err)

	_, err = builder.L1.EnsureTxSucceeded(tx)

	Require(t, err)

	checkPostUpgradeAssertions(t, proxyAdmin, callOpts, challengeManagerAddress, challengeManagerImplAddr, challengeManager, oldOspEntryAddress, newOspEntry)

	log.Info("Successfully upgraded smart contracts for espresso sequencing.")
}

func scheduleArbOSUpgrade(t *testing.T, auth *bind.TransactOpts, builder *NodeBuilder) {

	arbOwner, err := precompilesgen.NewArbOwner(common.HexToAddress("0x070"), builder.L2.Client)
	Require(t, err)

	test, err := precompilesgen.NewArbDebug(common.HexToAddress("0xff"), builder.L2.Client)
	Require(t, err)

	tx, err := test.BecomeChainOwner(auth)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	tx, err = arbOwner.ScheduleArbOSUpgrade(auth, 35, 0) // schedule the upgrade at timestamp 0 to upgrade instantly.
	Require(t, err)

	l2Node, l2Info, cleanL2Node := createL2Node(ctx, t, hotShotUrl, builder, false)

	return builder, l2Node, l2Info, cleanL2Node, func() {
		cleanup()
		cleanValNode()
	}
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
}

func TestEspressoMigration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder, l2Node, l2Info, _, cleanL2Node, cleanup := BuildNonEspressoNetwork(ctx, t)
	defer cleanup()
	node := builder.L2

	// Wait for the initial message
	expected := arbutil.MessageIndex(1)
	err := waitFor(t, ctx, func() bool {
		msgCnt, err := l2Node.ConsensusNode.TxStreamer.GetMessageCount()
		if err != nil {
			panic(err)
		}

		validatedCnt := node.ConsensusNode.BlockValidator.Validated(t)
		return msgCnt >= expected && validatedCnt >= expected
	})
	Require(t, err)

	auth := builder.L1Info.GetDefaultTransactOpts("RollupOwner", ctx)
	l2auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)

	preEspressoAccount := "preEspressoAccount"
	err = checkTransferTxOnL2(t, ctx, l2Node, preEspressoAccount, l2Info)
	Require(t, err)
	// Check if the tx is executed correctly
	err = checkTransferTxOnL2(t, ctx, l2Node, "User10", l2Info)
	Require(t, err)

	// Remember the number of messages
	var msgCnt arbutil.MessageIndex
	err = waitFor(t, ctx, func() bool {
		cnt, err := node.ConsensusNode.TxStreamer.GetMessageCount()
		Require(t, err)
		msgCnt = cnt
		log.Info("waiting for message count", "cnt", msgCnt)
		return msgCnt > 2
	})
	Require(t, err)

	// Wait for the number of validated messages to catch up
	err = waitForWith(t, ctx, 360*time.Second, 5*time.Second, func() bool {
		validatedCnt := node.ConsensusNode.BlockValidator.Validated(t)
		log.Info("waiting for validation", "validatedCnt", validatedCnt, "msgCnt", msgCnt)
		return validatedCnt >= msgCnt
	})
	Require(t, err)

	log.Info("Turn on espresso network after sequencing some default transactions.")

	log.Info("Update relevant contracts on L1 to prepare for espresso sequencing.")

	upgradeContracts(t, &auth, &l2auth, ctx, builder)

	log.Info("Successfully upgraded the challenge manager to point to the new OSP entry.")

	log.Info("Turn off non--espresso sequencing by stopping previous sequencer")

	err = waitFor(t, ctx, func() bool {
		cnt, err := node.ConsensusNode.TxStreamer.GetMessageCount()
		Require(t, err)
		msgCnt = cnt
		log.Info("waiting for message count", "cnt", msgCnt)
		return msgCnt > 2
	})
	Require(t, err)

	// Wait for the number of validated messages to catch up
	err = waitForWith(t, ctx, 360*time.Second, 5*time.Second, func() bool {
		validatedCnt := node.ConsensusNode.BlockValidator.Validated(t)
		log.Info("waiting for validation", "validatedCnt", validatedCnt, "msgCnt", msgCnt)
		return validatedCnt >= msgCnt
	})
	Require(t, err)
	//maybe wait here for contract upgrades?
	cleanL2Node()

	//	cleanL2Node()

	// create L2 node in espresso mode here

	l2Node, l2Info, _, cleanL2Node = createL2Node(ctx, t, hotShotUrl, builder, true)
	defer cleanL2Node()

	scheduleArbOSUpgrade(t, &l2auth, builder)

	postEspressoAccount := "postEspressoAccount1"
	err = checkTransferTxOnL2(t, ctx, builder.L2, postEspressoAccount, builder.L2Info)
	Require(t, err)

	postEspressoAccount2 := "postEspressoAccount2"
	err = checkTransferTxOnL2(t, ctx, builder.L2, postEspressoAccount2, builder.L2Info)
	Require(t, err)

	postEspressoAccount3 := "postEspressoAccount3"
	err = checkTransferTxOnL2(t, ctx, builder.L2, postEspressoAccount3, builder.L2Info)
	Require(t, err)

	postEspressoAccount4 := "postEspressoAccount4"
	err = checkTransferTxOnL2(t, ctx, builder.L2, postEspressoAccount4, builder.L2Info)
	Require(t, err)

	//TODO: the above transfers seem to execute successfully, but the validation count doesn't appear to increase, and various ammounts of validation error messages seem to be appearing during the test.

	// Remember the number of messages
	err = waitFor(t, ctx, func() bool {
		cnt, err := builder.L2.ConsensusNode.TxStreamer.GetMessageCount()
		Require(t, err)
		msgCnt = cnt
		log.Info("waiting for message count", "cnt", msgCnt)
		return msgCnt > 5
	})
	Require(t, err)

	// Wait for the number of validated messages to catch up
	err = waitForWith(t, ctx, 360*time.Second, 5*time.Second, func() bool {
		validatedCnt := builder.L2.ConsensusNode.BlockValidator.Validated(t)
		log.Info("waiting for validation", "validatedCnt", validatedCnt, "msgCnt", msgCnt)
		return validatedCnt >= msgCnt
	})
	Require(t, err)
	log.Info("Successfully ran the test")
}
