package submitter

import (
	"bytes"
	"context"
	"fmt"
	"time"

	espresso_client "github.com/EspressoSystems/espresso-network/sdks/go/client"
	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"

	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/util"
)

// TransactionIncludedJob represents a job to check if a transaction is included
// in Espresso.
type TransactionIncludedJob struct {
	txnHash        espresso_types.TaggedBase64
	txn            espresso_types.Transaction
	submitSuccess  time.Time
	submitAttempts uint
	attempt        uint
	lastAttempt    time.Time
}

// TransactionIncludedResponse represents a response / result of a performed
// TransactionIncludedJob task
type TransactionIncludedResponse struct {
	job                  TransactionIncludedJob
	transactionQueryData espresso_types.TransactionQueryData
	transactionsInBlock  espresso_client.TransactionsInBlock
	index                int
	err                  error
}

// transactionIncludedQueueWorker is a worker that processes
// TransactionIncludedJob tasks.
type transactionIncludedQueueWorker struct {
	id                          uint64
	chainID                     uint64
	client                      espresso_client.EspressoClient
	failurePenalty              time.Duration
	transactionIncludedJobQueue chan<- chan TransactionIncludedJob
	transactionIncludedResponse chan<- TransactionIncludedResponse
}

// newTransactionIncludedQueueWorker creates a new transaction included queue
// worker with the given ID, client, job queue, and response channel.
func newTransactionIncludedQueueWorker(
	id uint64, chainID uint64, client espresso_client.EspressoClient,
	failurePenalty time.Duration,
	transactionIncludedJobQueue chan<- chan TransactionIncludedJob,
	transactionIncludedResponse chan<- TransactionIncludedResponse,
) *transactionIncludedQueueWorker {
	return &transactionIncludedQueueWorker{
		id:                          id,
		chainID:                     chainID,
		client:                      client,
		failurePenalty:              failurePenalty,
		transactionIncludedJobQueue: transactionIncludedJobQueue,
		transactionIncludedResponse: transactionIncludedResponse,
	}
}

// startWorker is the function that actually performs the underlying work of
// the transaction included queue worker.
//
// It is responsible for checking if a transaction is included in Espresso,
// and handling the response from Espresso.
//
// NOTE: This worker does not observe the context cancellation, as it is
// expected to run until it is out of work.  Instead, it relies its job
// queue being closed by the worker scheduler to indicate that no more
// work is left for it to perform, then it will exit gracefully.
func (w *transactionIncludedQueueWorker) startWorker(_ context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan TransactionIncludedJob)
	for {
		// Submit our worker channel to the job queue
		w.transactionIncludedJobQueue <- ch
		// Wait for a job to be sent to us
		job, ok := <-ch
		if !ok {
			log.Warn("Transaction inclusion job queue closed, exiting", "worker", w.id)
			return
		}
		// Process the job

		if job.attempt > 0 {
			// If our attempts is greater than 0, it indicates that this isn't
			// our first attempt at processing this job.  We want to avoid a
			// scenario where we are hitting the Espresso Node with too many
			// requests at once.  Since our job queue doesn't have any sense
			// of time, the simplest way to handle this is just to sleep for
			// a little bit longer each time we re-attempt processing the job.
			// This will help to spread out the requests over time, and avoid
			// overwhelming the Espresso Node.
			//
			// By applying this penalty here we also avoid a potential very
			// active processing loop where we just requeue the job over and
			// over without being able to make any progress.
			log.Info("Re-attempting transaction inclusion check, applying delay penalty", "worker", w.id, "attempt", job.attempt, "delay", util.ConvertToInt64WithFallback[uint, time.Duration](job.attempt, 0)*w.failurePenalty)
			time.Sleep(util.ConvertToInt64WithFallback[uint, time.Duration](job.attempt, 0) * w.failurePenalty)
		}

		details, err := w.client.FetchTransactionByHash(ctx, &job.txnHash)
		if err != nil {
			w.transactionIncludedResponse <- TransactionIncludedResponse{
				job: job,
				err: ErrorFetchTransactionByHashFailed{Cause: err, WorkerID: w.id, TxnHash: job.txnHash},
			}
			continue
		}

		// We need to verify the transaction in the block
		txns, err := w.client.FetchTransactionsInBlock(ctx, details.BlockHeight, job.txn.Namespace)
		if err != nil {
			w.transactionIncludedResponse <- TransactionIncludedResponse{
				job:                  job,
				transactionQueryData: details,
				err:                  ErrorFetchTransactionsForBlockAndNamespaceFailed{Cause: err, WorkerID: w.id, TxnHash: job.txnHash, BlockHeight: details.BlockHeight, Namespace: job.txn.Namespace},
			}
			continue
		}

		// One of the transactions in the block should match the txn payload
		// we submitted
		if index, isIncluded := w.doesTransactionExistInBlock(job, txns); isIncluded {
			// We found the transaction in the block, so we can mark it as included
			w.transactionIncludedResponse <- TransactionIncludedResponse{
				job:                  job,
				transactionQueryData: details,
				transactionsInBlock:  txns,
				index:                index,
			}
			continue
		}

		// If we reach here, it means the transaction was not found in
		// the block
		w.transactionIncludedResponse <- TransactionIncludedResponse{
			job:                  job,
			transactionQueryData: details,
			transactionsInBlock:  txns,
			index:                -1,
			err:                  ErrorTransactionNotFoundInBlock{WorkerID: w.id, BlockHeight: details.BlockHeight, TxnHash: job.txnHash},
		}
	}
}

// doesTransactionExistInBlock checks if the transaction exists in the block
// represented by the TransactionsInBlock object.
//
// This check is done by comparing the transaction payload with the
// transactions in the block.  If a transaction with the same payload is found,
// it is considered to be included in the block.
func (w *transactionIncludedQueueWorker) doesTransactionExistInBlock(
	job TransactionIncludedJob, txns espresso_client.TransactionsInBlock,
) (int, bool) {
	for i, txn := range txns.Transactions {
		if bytes.Equal(txn, job.txn.Payload) {
			return i, true
		}
	}
	return -1, false
}

// ErrorFetchTransactionByHashFailed is an error that is returned when
// fetching a transaction by its hash fails.
//
// It wraps the original error that caused the failure, and includes the
// transaction hash that was being fetched.
type ErrorFetchTransactionByHashFailed struct {
	Cause    error
	WorkerID uint64
	TxnHash  espresso_types.TaggedBase64
}

// Error implements error
func (e ErrorFetchTransactionByHashFailed) Error() string {
	return fmt.Sprintf("failed to fetch transaction by hash %s: %v", e.TxnHash.String(), e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFetchTransactionByHashFailed) Unwrap() error {
	return e.Cause
}

// ErrorFetchTransactionsForBlockAndNamespaceFailed is an error that is
// returned when fetching transactions for a specific block and namespace
// fails.
type ErrorFetchTransactionsForBlockAndNamespaceFailed struct {
	Cause       error
	WorkerID    uint64
	BlockHeight uint64
	Namespace   uint64
	TxnHash     espresso_types.TaggedBase64
}

// Error implements error
func (e ErrorFetchTransactionsForBlockAndNamespaceFailed) Error() string {
	return fmt.Sprintf("failed to fetch transactions for block %d and namespace %d, determined from transaction hash: %s: %v", e.BlockHeight, e.Namespace, e.TxnHash.String(), e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFetchTransactionsForBlockAndNamespaceFailed) Unwrap() error {
	return e.Cause
}

// ErrorTransactionNotFoundInBlock is an error that is returned when a
// transaction is not found in a specific block.
type ErrorTransactionNotFoundInBlock struct {
	WorkerID    uint64
	TxnHash     espresso_types.TaggedBase64
	BlockHeight uint64
}

// Error implements error
func (e ErrorTransactionNotFoundInBlock) Error() string {
	return fmt.Sprintf("transaction %s not found in block %d", e.TxnHash.String(), e.BlockHeight)
}
