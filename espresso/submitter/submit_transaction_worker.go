package submitter

import (
	"context"
	"fmt"
	"time"

	espresso_client "github.com/EspressoSystems/espresso-network/sdks/go/client"
	tagged_base64 "github.com/EspressoSystems/espresso-network/sdks/go/tagged-base64"
	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/util"
)

// SubmitTransactionJob represents a job to submit a transaction to Espresso.
type SubmitTransactionJob struct {
	txn         espresso_types.Transaction
	attempt     uint
	lastAttempt time.Time
}

// SubmitTransactionResponse represents a response / result of a performed
// SubmitTransactionJob task
type SubmitTransactionResponse struct {
	job  SubmitTransactionJob
	hash *espresso_types.TaggedBase64
	err  error
}

// submitTransactionWorker is a worker that processes SubmitTransactionJob
// tasks.
//
// It is **only** responsible for submitting transactions to Espresso, and
// returning the response back to be handled.
type submitTransactionWorker struct {
	id                 uint64
	client             espresso_client.EspressoClient
	submitTxnsJobQueue chan<- chan SubmitTransactionJob
	submitTxnsResponse chan<- SubmitTransactionResponse
}

// newSubmitTransactionWorker creates a new submit transaction worker with the
// given ID, client, job queue, and response channel.
func newSubmitTransactionWorker(id uint64, client espresso_client.EspressoClient,
	submitTxnsJobQueue chan<- chan SubmitTransactionJob,
	submitTxnsResponse chan<- SubmitTransactionResponse) *submitTransactionWorker {
	return &submitTransactionWorker{
		id:                 id,
		client:             client,
		submitTxnsJobQueue: submitTxnsJobQueue,
		submitTxnsResponse: submitTxnsResponse,
	}
}

// startWorker is the function that actually performs the underlying work of
// the submit transaction worker.
//
// It is responsible for submitting transactions to Espresso, and handling
// the response from Espresso.
//
// NOTE: This worker does not observe the context cancellation, as it is
// expected to run until it is out of work.  Instead, it relies its job
// queue being closed by the scheduler to indicate that no more work is left
// for it to perform, then it will exit gracefully.
func (w *submitTransactionWorker) startWorker(_ context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan SubmitTransactionJob)

	for {
		// Submit our worker channel to the job queue
		w.submitTxnsJobQueue <- ch
		// Wait for a job to be sent to us
		job, ok := <-ch
		if !ok {
			log.Warn("Submit transaction job queue closed, exiting", "worker", w.id)
			return
		}

		// estimate the hash
		commit := job.txn.Commit()
		estimatedHash, err := tagged_base64.New("TX", commit[:])
		if err != nil {
			// This is unfortunate, but we shouldn't stop processing the job
			// just because of a small reference failure.
			log.Warn("Failed to estimate transaction hash", "error", err, "commit", common.Hash(commit), "worker", w.id)
			estimatedHash = new(tagged_base64.TaggedBase64)
		}

		// Process the job
		if job.attempt > 0 {
			// Let's slow down a little
			time.Sleep(util.ConvertToInt64WithFallback[uint, time.Duration](job.attempt, 0) * 100 * time.Millisecond)
		}

		log.Info("Submitting transaction to Espresso", "commit", common.Hash(job.txn.Commit()), "worker", w.id)

		hash, err := w.client.SubmitTransaction(ctx, job.txn)
		response := SubmitTransactionResponse{
			job:  job,
			hash: hash,
		}
		if err != nil {
			response.err = ErrorSubmitTransactionFailed{Cause: err, WorkerID: w.id, EstimatedHash: *estimatedHash}
		}

		// Send the response back to the response handler
		w.submitTxnsResponse <- response
	}
}

// ErrorSubmitTransactionFailed is an error that is returned when submitting a
// transaction to Espresso fails.
type ErrorSubmitTransactionFailed struct {
	Cause         error
	WorkerID      uint64
	EstimatedHash espresso_types.TaggedBase64
}

// Error implements error
func (e ErrorSubmitTransactionFailed) Error() string {
	return fmt.Sprintf("failed to submit transaction %s: %v", e.EstimatedHash.String(), e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorSubmitTransactionFailed) Unwrap() error {
	return e.Cause
}
