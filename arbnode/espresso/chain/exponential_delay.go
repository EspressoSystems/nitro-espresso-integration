package chain

import (
	"context"
	"math"
	"time"

	espresso_client "github.com/EspressoSystems/espresso-network/sdks/go/client"
	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	espresso_common "github.com/EspressoSystems/espresso-network/sdks/go/types/common"
	"github.com/offchainlabs/nitro/arbnode/espresso"
)

type EspressoChainExponentialDelay struct {
	chain espresso.TransactionStreamerEspressoClient
}

// NewEspressoChainExponentialDelay creates a new instance of
// EspressoChainExponentialDelay with the specified chain client. This
// wrapper introduces an exponential delay

func NewEspressoChainExponentialDelay(chain espresso.TransactionStreamerEspressoClient) *EspressoChainExponentialDelay {
	return &EspressoChainExponentialDelay{
		chain: chain,
	}
}

// FetchTransactionsInBlock implements espresso.TransactionStreamerEspressoClient
func (c *EspressoChainExponentialDelay) FetchTransactionsInBlock(ctx context.Context, blockHeight uint64, namespace uint64) (espresso_client.TransactionsInBlock, error) {
	return c.chain.FetchTransactionsInBlock(ctx, blockHeight, namespace)
}

// FetchTransactionsInBlock implements espresso.TransactionStreamerEspressoClient
func (c *EspressoChainExponentialDelay) FetchTransactionByHash(ctx context.Context, hash *espresso_types.TaggedBase64) (espresso_types.TransactionQueryData, error) {
	result, err := c.chain.FetchTransactionByHash(ctx, hash)
	bytes := len(result.Transaction.Payload)
	// delay := time.Duration(bytes * bytes)

	// Exponential Curve Simulation.
	// 900,000 bytes should map to a 30 second delay.
	// 100,000 bytes should map to a 4.5 second delay

	const (
		_ = 1_000_000_000
		_ = 900_000
	)

	// Normalize the bytes to a range between 0 and 1
	delay := time.Duration(float64(time.Second) * 30 * math.Atan(math.Pi*float64(bytes)/float64(900_000)) / 1.5)

	// delay := time.Duration(math.Pow(float64(bytes), 1.75))

	// delay := time.Duration(bytes + (bytes / 2))

	time.Sleep(delay)

	return result, err
}

// FetchTransactionsInBlock implements espresso.TransactionStreamerEspressoClient
func (c *EspressoChainExponentialDelay) SubmitTransaction(ctx context.Context, tx espresso_common.Transaction) (*espresso_common.TaggedBase64, error) {
	return c.chain.SubmitTransaction(ctx, tx)
}
