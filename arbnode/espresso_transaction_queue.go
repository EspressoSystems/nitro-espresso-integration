package arbnode

import (
	"context"
	"slices"
	"sync"
	"time"

	espressoClient "github.com/EspressoSystems/espresso-sequencer-go/client"
	espressoTypes "github.com/EspressoSystems/espresso-sequencer-go/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/util/stopwaiter"
	flag "github.com/spf13/pflag"
)

type EspressoTransactionQueue struct {
	stopwaiter.StopWaiter

	config EspressoTransactionQueueConfigFetcher

	espressoClient *espressoClient.Client
	newTxnNotifier chan struct{}

	pendingTxnsQueueMutex sync.Mutex // cannot be acquired while reorgMutex is held
	pendingTxnsQueue      []*espressoTypes.Transaction

	submittedTxn *espressoTypes.Transaction
}

type EspressoTransactionQueueConfig struct {
	HotShotUrl                  string        `koanf:"hotshot-url"`
	EspressoNamespace           uint64        `koanf:"espresso-namespace"`
	EspressoTxnsPollingInterval time.Duration `koanf:"espresso-polling-interval"`
}

type EspressoTransactionQueueConfigFetcher func() *EspressoTransactionQueueConfig

var DefaultEspressoTransactionQueueConfig = EspressoTransactionQueueConfig{
	HotShotUrl:                  "",
	EspressoTxnsPollingInterval: time.Millisecond * 100,
}

var TestEspressoTransactionQueueConfig = EspressoTransactionQueueConfig{
	HotShotUrl:                  "",
	EspressoTxnsPollingInterval: time.Millisecond,
}

func EspressoTransactionQueueConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.String(prefix+".hotshot-url", DefaultEspressoTransactionQueueConfig.HotShotUrl, "url of the hotshot sequencer")
	f.Uint64(prefix+".espresso-namespace", DefaultEspressoTransactionQueueConfig.EspressoNamespace, "espresso namespace that corresponds the L2 chain")
	f.Duration(prefix+".espresso-polling-interval", DefaultEspressoTransactionQueueConfig.EspressoTxnsPollingInterval, "interval between polling for transactions to be included in the block")
}

func NewEspressoTransactionQueue(config EspressoTransactionQueueConfigFetcher) *EspressoTransactionQueue {

	espressoClient := espressoClient.NewClient(config().HotShotUrl)

	return &EspressoTransactionQueue{
		espressoClient:        espressoClient,
		pendingTxnsQueue:      make([]*espressoTypes.Transaction, 0),
		pendingTxnsQueueMutex: sync.Mutex{},
		config:                config,
	}
}

func (q *EspressoTransactionQueue) processEspressoTransactions(ctx context.Context, ignored struct{}) time.Duration {

	if q.submittedTxn != nil {
		// TODO: not sure if this is the correct block height or not arbitrum vs espresso
		latestBlock, err := q.espressoClient.FetchLatestBlockHeight(ctx)
		if err != nil {
			log.Error("failed to fetch latest espresso block height", "err", err)
			return q.config().EspressoTxnsPollingInterval
		}
		// check if each transaction is included in the block and then remove it from the queue
		fetchedTransactions, err := q.espressoClient.FetchTransactionsInBlock(context.Background(), latestBlock, q.config().EspressoNamespace)
		if err != nil {
			log.Error("failed to fetch transactions in espresso block", "err", err)
			return q.config().EspressoTxnsPollingInterval
		}

		for _, fetchedfetchedTransactions := range fetchedTransactions.Transactions {
			if slices.Equal(fetchedfetchedTransactions, q.submittedTxn.Payload) {
				q.submittedTxn = nil
			}
		}
	}

	q.pendingTxnsQueueMutex.Lock()
	defer q.pendingTxnsQueueMutex.Unlock()

	if q.submittedTxn == nil && len(q.pendingTxnsQueue) > 0 {
		err := q.espressoClient.SubmitTransaction(ctx, *q.pendingTxnsQueue[0])
		if err != nil {
			log.Error("failed to submit transaction to espresso", "err", err)
			return q.config().EspressoTxnsPollingInterval
		}

		q.submittedTxn = q.pendingTxnsQueue[0]
		q.pendingTxnsQueue = q.pendingTxnsQueue[1:]
	}

	return q.config().EspressoTxnsPollingInterval
}

func (q *EspressoTransactionQueue) SubmitTransaction(ctx context.Context, txn *espressoTypes.Transaction) {
	q.pendingTxnsQueueMutex.Lock()
	defer q.pendingTxnsQueueMutex.Unlock()
	q.pendingTxnsQueue = append(q.pendingTxnsQueue, txn)
	q.newTxnNotifier <- struct{}{}
}

func (q *EspressoTransactionQueue) Start(ctxIn context.Context) error {
	q.StopWaiter.Start(ctxIn, q)
	return stopwaiter.CallIterativelyWith[struct{}](&q.StopWaiterSafe, q.processEspressoTransactions, q.newTxnNotifier)

}
