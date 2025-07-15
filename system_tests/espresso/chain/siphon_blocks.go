package chain

import (
	"context"

	espresso_client "github.com/EspressoSystems/espresso-network/sdks/go/client"
	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	espresso_common "github.com/EspressoSystems/espresso-network/sdks/go/types/common"
	"github.com/offchainlabs/nitro/espresso"
)

type SiphonBlocksWithTransactions struct {
	chain espresso.TransactionStreamerEspressoClient
	ch    chan<- espresso_client.TransactionsInBlock
}

func NewSiphonBlocksWithTransactions(
	chain espresso.TransactionStreamerEspressoClient,
	ch chan<- espresso_client.TransactionsInBlock,
) *SiphonBlocksWithTransactions {
	return &SiphonBlocksWithTransactions{
		chain: chain,
		ch:    ch,
	}
}

func (c *SiphonBlocksWithTransactions) FetchTransactionsInBlock(ctx context.Context, blockHeight uint64, namespace uint64) (espresso_client.TransactionsInBlock, error) {
	result, err := c.chain.FetchTransactionsInBlock(ctx, blockHeight, namespace)
	if err != nil {
		return result, err
	}

	// Send the result to the channel
	c.ch <- result
	return result, err
}

// FetchTransactionByHash implements espresso.TransactionStreamerEspressoClient
func (c *SiphonBlocksWithTransactions) FetchTransactionByHash(ctx context.Context, hash *espresso_types.TaggedBase64) (espresso_types.TransactionQueryData, error) {
	return c.chain.FetchTransactionByHash(ctx, hash)
}

// Advance implements espresso.TransactionStreamerEspressoClient
func (c *SiphonBlocksWithTransactions) SubmitTransaction(ctx context.Context, tx espresso_common.Transaction) (*espresso_common.TaggedBase64, error) {
	return c.chain.SubmitTransaction(ctx, tx)
}
