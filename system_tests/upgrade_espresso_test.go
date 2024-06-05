package arbtest

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/params"
	"github.com/offchainlabs/nitro/solgen/go/rollupgen"
	"github.com/offchainlabs/nitro/solgen/go/test_helpersgen"
	"github.com/offchainlabs/nitro/solgen/go/upgrade_executorgen"
)

func TestUpgradeEspresso(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initialBalance := new(big.Int).Lsh(big.NewInt(1), 200)
	l1Info := NewL1TestInfo(t)
	l1Info.GenerateGenesisAccount("deployer", initialBalance)

	deployerTxOpts := l1Info.GetDefaultTransactOpts("deployer", ctx)

	chainConfig := params.ArbitrumDevTestChainConfig()
	l1Info, l1Backend, _, _ := createTestL1BlockChain(t, l1Info)
	hotshotAddr, tx, _, err := test_helpersgen.DeployMockHotShot(&deployerTxOpts, l1Backend)
	Require(t, err)
	_, err = EnsureTxSucceeded(ctx, l1Backend, tx)
	Require(t, err)

	rollup, _ := DeployOnTestL1(t, ctx, l1Info, l1Backend, chainConfig, hotshotAddr)
	deployAuth := l1Info.GetDefaultTransactOpts("RollupOwner", ctx)

	upgradeExecutor, err := upgrade_executorgen.NewUpgradeExecutor(rollup.UpgradeExecutor, l1Backend)
	rollupABI, err := abi.JSON(strings.NewReader(rollupgen.RollupAdminLogicABI))
	Require(t, err)
	upgradeContractCalldata, err := rollupABI.Pack("upgradeTo", rollup.Rollup)
	Require(t, err, "unable to generate upgradeContractCalldata calldata")
	tx, err = upgradeExecutor.ExecuteCall(&deployAuth, rollup.Rollup, upgradeContractCalldata)
	Require(t, err, "unable to upgrade contract")
	_, err = EnsureTxSucceeded(ctx, l1Backend, tx)
	Require(t, err)
}
