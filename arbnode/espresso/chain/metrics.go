package chain

import (
	"context"
	"sync"
	"time"

	espresso_client "github.com/EspressoSystems/espresso-network/sdks/go/client"
	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	espresso_common "github.com/EspressoSystems/espresso-network/sdks/go/types/common"
	"github.com/offchainlabs/nitro/arbnode/espresso"
)

type EspressoChainClientMetrics struct {
	lock  sync.RWMutex
	chain espresso.TransactionStreamerEspressoClient

	// FetchTransactionsInBlockRequests
	TransactionsInBlockRequestsMetrics map[FetchTransactionsInBlockRequest][]TimingData

	// FetchTransactionsByHashRequests
	TransactionsByHashMetrics map[string][]TimingData

	// SubmitTransactionRequests
	SubmitTransactionMetrics map[string][]TimingData
}

type FetchTransactionsInBlockRequest struct {
	BlockHeight uint64
	Namespace   uint64
}

type TimingData struct {
	Duration time.Duration
	Start    time.Time
	End      time.Time
}

func Timing(start, end time.Time) TimingData {
	return TimingData{
		Duration: end.Sub(start),
		Start:    start,
		End:      end,
	}
}

type FetchTransactionsInBlockRequestRecord struct {
	BlockHeight uint64
	Namespace   uint64
	Start       time.Time
	End         time.Time
}

func NewEspressoChainMetrics(chain espresso.TransactionStreamerEspressoClient) *EspressoChainClientMetrics {
	return &EspressoChainClientMetrics{
		chain:                              chain,
		TransactionsInBlockRequestsMetrics: make(map[FetchTransactionsInBlockRequest][]TimingData),
		TransactionsByHashMetrics:          make(map[string][]TimingData),
		SubmitTransactionMetrics:           make(map[string][]TimingData),
	}
}

func (c *EspressoChainClientMetrics) FetchTransactionsInBlock(ctx context.Context, blockHeight uint64, namespace uint64) (espresso_client.TransactionsInBlock, error) {
	start := time.Now()
	result, err := c.chain.FetchTransactionsInBlock(ctx, blockHeight, namespace)
	end := time.Now()

	request := FetchTransactionsInBlockRequest{
		BlockHeight: blockHeight,
		Namespace:   namespace,
	}

	c.lock.Lock()
	c.TransactionsInBlockRequestsMetrics[request] = append(c.TransactionsInBlockRequestsMetrics[request], Timing(start, end))
	c.lock.Unlock()

	return result, err
}

// FetchTransactionByHash implements espresso.TransactionStreamerEspressoClient
func (c *EspressoChainClientMetrics) FetchTransactionByHash(ctx context.Context, hash *espresso_types.TaggedBase64) (espresso_types.TransactionQueryData, error) {
	start := time.Now()
	result, err := c.chain.FetchTransactionByHash(ctx, hash)
	end := time.Now()

	hashStr := hash.String()
	c.lock.Lock()
	c.TransactionsByHashMetrics[hashStr] = append(c.TransactionsByHashMetrics[hashStr], Timing(start, end))
	c.lock.Unlock()

	return result, err
}

// Advance implements espresso.TransactionStreamerEspressoClient
func (c *EspressoChainClientMetrics) SubmitTransaction(ctx context.Context, tx espresso_common.Transaction) (*espresso_common.TaggedBase64, error) {
	tag, err := TransactionTaggedBase64(tx)
	if err != nil {
		return nil, err
	}

	hashStr := tag.String()

	start := time.Now()
	result, err := c.chain.SubmitTransaction(ctx, tx)
	end := time.Now()

	c.lock.Lock()
	c.SubmitTransactionMetrics[hashStr] = append(c.SubmitTransactionMetrics[hashStr], Timing(start, end))
	c.lock.Unlock()

	return result, err
}
