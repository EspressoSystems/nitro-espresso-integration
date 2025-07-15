package chain

import (
	"context"
	"time"

	espresso_client "github.com/EspressoSystems/espresso-network/sdks/go/client"
	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	espresso_common "github.com/EspressoSystems/espresso-network/sdks/go/types/common"
	"github.com/offchainlabs/nitro/arbnode/espresso"
)

// EspressoChainDelayed is a wrapper around an Espresso chain client that
// introduces a delay before executing any method calls. This can be useful for
// simulating network latency or for testing purposes.
type EspressoChainDelayed struct {
	chain                    espresso.TransactionStreamerEspressoClient
	transactionsInBlockDelay time.Duration
	transactionsByHashDelay  time.Duration
	submitTransactionDelay   time.Duration
}

// NewEspressoChainDelayed creates a new instance of EspressoChainDelayed with
// the specified chain client and delay duration. The delay will be applied to
// all method calls made to the chain client.
func NewEspressoChainDelayed(chain espresso.TransactionStreamerEspressoClient, transactionsInBlockDelay, transactionsByHashDelay, submitTransactionDelay time.Duration) *EspressoChainDelayed {
	return &EspressoChainDelayed{
		chain:                    chain,
		transactionsInBlockDelay: transactionsInBlockDelay,
		transactionsByHashDelay:  transactionsByHashDelay,
		submitTransactionDelay:   submitTransactionDelay,
	}
}

var _ espresso.TransactionStreamerEspressoClient = &MockEspressoChain{}

// simulateDelay simulates a delay by sleeping for the specified duration.
func (c *EspressoChainDelayed) simulateDelay(delay time.Duration) {
	time.Sleep(delay)
}

// FetchTransactionsInBlock implements espresso.TransactionStreamerEspressoClient
func (c *EspressoChainDelayed) FetchTransactionsInBlock(ctx context.Context, blockHeight uint64, namespace uint64) (espresso_client.TransactionsInBlock, error) {
	c.simulateDelay(c.transactionsInBlockDelay)
	return c.chain.FetchTransactionsInBlock(ctx, blockHeight, namespace)
}

// FetchTransactionByHash implements espresso.TransactionStreamerEspressoClient
func (c *EspressoChainDelayed) FetchTransactionByHash(ctx context.Context, hash *espresso_types.TaggedBase64) (espresso_types.TransactionQueryData, error) {
	c.simulateDelay(c.transactionsByHashDelay)
	return c.chain.FetchTransactionByHash(ctx, hash)
}

// Advance implements espresso.TransactionStreamerEspressoClient
func (c *EspressoChainDelayed) SubmitTransaction(ctx context.Context, tx espresso_common.Transaction) (*espresso_common.TaggedBase64, error) {
	time.Sleep(c.submitTransactionDelay)
	return c.chain.SubmitTransaction(ctx, tx)
}
