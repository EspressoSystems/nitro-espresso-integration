package submitter

import (
	"context"
	"fmt"
	"time"

	espresso_client "github.com/EspressoSystems/espresso-network/sdks/go/client"
	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

// MultiWorkerQueueEspressoSubmitter is an implementation of `EspressoSubmitter`
// that utilizes multiple worker queues to perform Transaction Submission to
// Espresso, and ensure its inclusion.
//
// It approaches the problem by dividing up the task into two separate phases,
// each of which can be scaled independently as needed.  The two phases are
// 1. Submit Transaction Phase: This phase is responsible ensuring that a
// transaction is submitted to Espresso successfully.
// 2. Transaction Inclusion Phase: This phase is responsible for checking if
// a transaction is included in Espresso.
//
// The approach focuses on two main ideas, split up work into two separate
// discrete working units, and allow each unit to scale independently, and
// operate in parallel.
//
// By splitting the work into distinct phases, despite them being related,
// we can ensure that each worker only needs to focus on a single task without
// distracting itself with the other phase, and complicating the logic.
//
// If a Transaction has been successfully submitted to Espresso, but has not
// been included in Espresso within a specified deadline, it will be demoted
// and moved back into the Submission phase, where it will be submitted again
// as if it never was submitted before.
type MultiWorkerQueueEspressoSubmitter struct {
	client espresso_client.EspressoClient

	chainID                       uint64
	resubmissionDeadline          time.Duration
	numSubmitTransactionWorkers   uint64
	numTransactionIncludedWorkers uint64

	submitTxnsQueue    chan SubmitTransactionJob
	submitTxnsJobQueue chan chan SubmitTransactionJob
	submitTxnsResponse chan SubmitTransactionResponse

	transactionIncludedQueue    chan TransactionIncludedJob
	transactionIncludedJobQueue chan chan TransactionIncludedJob
	transactionIncludedResponse chan TransactionIncludedResponse
}

// ErrorFailedToCreateMultiWorkerQueueEspressoSubmitter is an error that is
// returned when the multi-worker queue espresso submitter fails to be created.
type ErrorFailedToCreateMultiWorkerQueueEspressoSubmitter struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToCreateMultiWorkerQueueEspressoSubmitter) Error() string {
	return fmt.Sprintf("failed to create multi-worker queue espresso submitter: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToCreateMultiWorkerQueueEspressoSubmitter) Unwrap() error {
	return e.Cause
}

// NewMultiWorkerQueueEspressoSubmitter creates a new WorkerQueues instance
// with the provided options.
func NewMultiWorkerQueueEspressoSubmitter(options ...EspressoSubmitterConfigOption) (EspressoSubmitter, error) {
	config := DefaultEspressoSubmitterConfig
	applyEspressoSubmitterConfigOptions(&config, options...)
	if err := ValidateEspressoSubmitterConfig(config); err != nil {
		return nil, ErrorFailedToCreateMultiWorkerQueueEspressoSubmitter{Cause: err}
	}

	return &NitroMessageToEspressoTransactionAdapter{
		db:                         config.Db,
		chainID:                    config.ChainID,
		availableTransaction:       make(chan arbutil.MessageIndex, config.MessageIndexQueueSize),
		messageGetter:              config.MessageGetter,
		sendingInterval:            config.EspressoTxnSendingInterval,
		keyManager:                 config.KeyManager,
		userDataAttestationFile:    config.UserDataAttestationFile,
		quoteFile:                  config.QuoteFile,
		espressoMaxTransactionSize: config.EspressoMaxTransactionSize,
		submitter: &MultiWorkerQueueEspressoSubmitter{
			chainID:                       config.ChainID,
			resubmissionDeadline:          config.ResubmitEspressoTxDeadline,
			numSubmitTransactionWorkers:   config.NumberOfSubmitTransactionWorkers,
			numTransactionIncludedWorkers: config.NumberOfTransactionIncludedWorkers,
			client:                        config.EspressoClient,
			submitTxnsQueue:               make(chan SubmitTransactionJob, config.SubmitTransactionsQueueSize),
			transactionIncludedQueue:      make(chan TransactionIncludedJob, config.TransactionIncludedQueueSize),
		},
	}, nil
}

func (w *MultiWorkerQueueEspressoSubmitter) SubmitTransaction(txn espresso_types.Transaction) {
	// Let's submit this transaction to the job queue
	w.submitTxnsQueue <- SubmitTransactionJob{
		txn: txn,
	}
}

// ErrorFailedToLaunchSubmitTransactionQueueWorker is an error that is returned
// when the submit transaction queue worker fails to launch.
type ErrorFailedToLaunchSubmitTransactionQueueWorker struct {
	Cause  error
	Worker uint64
}

// Error implements error
func (e ErrorFailedToLaunchSubmitTransactionQueueWorker) Error() string {
	return fmt.Sprintf("failed to launch submit transaction queue worker %d: %v", e.Worker, e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchSubmitTransactionQueueWorker) Unwrap() error {
	return e.Cause
}

// ErrorFailedToLaunchSubmitTransactionScheduler is an error that is returned
// when the submit transaction scheduler fails to launch.
type ErrorFailedToLaunchSubmitTransactionScheduler struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToLaunchSubmitTransactionScheduler) Error() string {
	return fmt.Sprintf("failed to launch submit transaction scheduler: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchSubmitTransactionScheduler) Unwrap() error {
	return e.Cause
}

// ErrorFailedToLaunchSubmitTransactionResponseHandler is an error that is
// returned when the submit transaction response handler fails to launch.
type ErrorFailedToLaunchSubmitTransactionResponseHandler struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToLaunchSubmitTransactionResponseHandler) Error() string {
	return fmt.Sprintf("failed to launch submit transaction response handler: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchSubmitTransactionResponseHandler) Unwrap() error {
	return e.Cause
}

func (w *MultiWorkerQueueEspressoSubmitter) startSubmitTransactionProcess(sw *stopwaiter.StopWaiter) error {
	// Launch the workers, scheduler, and responser handler for submit transaction
	for i := uint64(0); i < w.numSubmitTransactionWorkers; i++ {
		if err := sw.LaunchThreadSafe(newSubmitTransactionWorker(i, w.client, w.submitTxnsJobQueue, w.submitTxnsResponse).startWorker); err != nil {
			return ErrorFailedToLaunchSubmitTransactionQueueWorker{Worker: i, Cause: err}
		}
	}

	if err := sw.LaunchThreadSafe(w.submitTransactionScheduler); err != nil {
		return ErrorFailedToLaunchSubmitTransactionScheduler{Cause: err}
	}

	if err := sw.LaunchThreadSafe(w.submitTransactionResponseHandler); err != nil {
		return ErrorFailedToLaunchSubmitTransactionResponseHandler{Cause: err}
	}

	return nil
}

// ErrorFailedToLaunchTransactionIncludedQueueWorker is an error that is
// returned when the transaction included queue worker fails to launch.
type ErrorFailedToLaunchTransactionIncludedQueueWorker struct {
	Cause  error
	Worker uint64
}

// Error implements error
func (e ErrorFailedToLaunchTransactionIncludedQueueWorker) Error() string {
	return fmt.Sprintf("failed to launch transaction included queue worker %d: %v", e.Worker, e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchTransactionIncludedQueueWorker) Unwrap() error {
	return e.Cause
}

// ErrorFailedToLaunchTransactionIncludedScheduler is an error that is returned
// when the transaction included scheduler fails to launch.
type ErrorFailedToLaunchTransactionIncludedScheduler struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToLaunchTransactionIncludedScheduler) Error() string {
	return fmt.Sprintf("failed to launch transaction included scheduler: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchTransactionIncludedScheduler) Unwrap() error {
	return e.Cause
}

// ErrorFailedToLaunchTransactionIncludedResponseHandler is an error that is
// returned when the transaction included response handler fails to launch.
type ErrorFailedToLaunchTransactionIncludedResponseHandler struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToLaunchTransactionIncludedResponseHandler) Error() string {
	return fmt.Sprintf("failed to launch transaction included response handler: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToLaunchTransactionIncludedResponseHandler) Unwrap() error {
	return e.Cause
}

// startTransactionIncludedProcess launches the transaction included process
// in a new goroutine using the provided stop waiter.
func (w *MultiWorkerQueueEspressoSubmitter) startTransactionIncludedProcess(sw *stopwaiter.StopWaiter) error {
	for i := uint64(0); i < w.numTransactionIncludedWorkers; i++ {
		if err := sw.LaunchThreadSafe(newTransactionIncludedQueueWorker(i, w.chainID, w.client, w.transactionIncludedJobQueue, w.transactionIncludedResponse).startWorker); err != nil {
			return ErrorFailedToLaunchTransactionIncludedQueueWorker{Worker: i, Cause: err}
		}
	}

	if err := sw.LaunchThreadSafe(w.transactionIncludedScheduler); err != nil {
		return ErrorFailedToLaunchTransactionIncludedScheduler{Cause: err}
	}

	if err := sw.LaunchThreadSafe(w.transactionIncludedResponseHandler); err != nil {
		return ErrorFailedToLaunchTransactionIncludedResponseHandler{Cause: err}
	}

	return nil
}

// ErrorFailedToStart is an error that is returned when the whole worker
// process fails to start. This can happen if any of the individual components
// fail to launch.
type ErrorFailedToStart struct {
	Cause error
}

// Error implements error
func (e ErrorFailedToStart) Error() string {
	return fmt.Sprintf("failed to start whole worker process: %v", e.Cause)
}

// Unwrap provides the underlying error for builtin errors checking
func (e ErrorFailedToStart) Unwrap() error {
	return e.Cause
}

// Start implements EspressoSubmitter.
func (w *MultiWorkerQueueEspressoSubmitter) Start(sw *stopwaiter.StopWaiter) error {
	w.submitTxnsJobQueue = make(chan chan SubmitTransactionJob, w.numSubmitTransactionWorkers)
	w.submitTxnsResponse = make(chan SubmitTransactionResponse, 1024)

	w.transactionIncludedJobQueue = make(chan chan TransactionIncludedJob, w.numTransactionIncludedWorkers)
	w.transactionIncludedResponse = make(chan TransactionIncludedResponse, 1024)

	// Start the submit transaction scheduler, response handlers, and workers
	if err := w.startSubmitTransactionProcess(sw); err != nil {
		return ErrorFailedToStart{Cause: err}
	}

	// Launch the workers, scheduler, and response handler for transaction inclusion
	if err := w.startTransactionIncludedProcess(sw); err != nil {
		return ErrorFailedToStart{Cause: err}
	}

	return nil
}

// drainSubmitTransactionWorkers closes all the worker channels in the
// submit transaction job queue, so that the workers can exit gracefully.
func (w *MultiWorkerQueueEspressoSubmitter) drainSubmitTransactionWorkers() {
	defer close(w.submitTxnsJobQueue)

	// We need to close the submit transaction job queue, so that the workers
	// can exit gracefully.
	for i := uint64(0); i < w.numSubmitTransactionWorkers; i++ {
		worker, workerOk := <-w.submitTxnsJobQueue
		if !workerOk {
			log.Warn("Submit transaction job queue already closed, exiting")
			// The job queue is closed, so we can exit.
			return
		}

		// Close the job queue of the worker, to indicate to it that
		// there are no more jobs to process
		close(worker)
	}
}

// submitTransactionScheduler is the scheduler for the submit transaction
// job queue.
func (w *MultiWorkerQueueEspressoSubmitter) submitTransactionScheduler(ctx context.Context) {
	defer w.drainSubmitTransactionWorkers()

	for {
		select {
		case <-ctx.Done():
			log.Warn("Submit transaction scheduler exiting")
			return

		case job, ok := <-w.submitTxnsQueue:
			// We have a job to process, so let's get a worker, and send the
			// job to it.

			if !ok {
				log.Warn("Submit transaction job queue closed, exiting")
				return
			}

			worker := <-w.submitTxnsJobQueue
			worker <- job
		}
	}
}

// submitTransactionResponseHandler is the response handler for the submit
// transaction job queue.
//
// It is responsible for determining the outcome of the performed job, and
// rescheduling the job upon failure if necessary, or moving the job to the
// transaction inclusion job queue if the job was successful.
func (w *MultiWorkerQueueEspressoSubmitter) submitTransactionResponseHandler(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Warn("Submit transaction response handler exiting")
			return

		case response := <-w.submitTxnsResponse:
			// We have a response to process, so let's handle it.
			// This will likely involve updating the state of the
			// submitter, and possibly notifying other components.
			if response.err != nil || response.hash == nil {
				// Handle error case
				job := response.job
				log.Warn("Failed to submit transaction to Espresso", "error", response.err, "attempts", job.attempt, "commit", common.Hash(job.txn.Commit()))
				job.attempt++
				job.lastAttempt = time.Now()
				w.submitTxnsQueue <- job
				continue
			}

			log.Info("Transaction successfully submitted to Espresso", "hash", response.hash.String(), "commit", common.Hash(response.job.txn.Commit()))

			// The job was successfully submitted, let's move to verification
			w.transactionIncludedQueue <- TransactionIncludedJob{
				txnHash:        *response.hash,
				txn:            response.job.txn,
				submitSuccess:  time.Now(),
				submitAttempts: response.job.attempt,
			}
		}
	}
}

// drainTransactionInclusionWorkers closes all the worker channels in the
// transaction inclusion job queue, so that the workers can exit gracefully.
func (w *MultiWorkerQueueEspressoSubmitter) drainTransactionInclusionWorkers() {
	defer close(w.transactionIncludedJobQueue)

	// We need to close the submit transaction job queue, so that the workers
	// can exit gracefully.
	for i := uint64(0); i < w.numTransactionIncludedWorkers; i++ {
		worker, workerOk := <-w.transactionIncludedJobQueue
		if !workerOk {
			// The job queue is closed, so we can exit.
			log.Warn("Transaction inclusion job queue already closed, exiting")
			return
		}

		// Close the job queue of the worker, to indicate to it that
		// there are no more jobs to process
		close(worker)
	}
}

// transactionIncludedScheduler is the scheduler for the transaction inclusion
// job queue.
func (w *MultiWorkerQueueEspressoSubmitter) transactionIncludedScheduler(ctx context.Context) {
	defer w.drainTransactionInclusionWorkers()

	for {
		select {
		case <-ctx.Done():
			log.Info("Transaction inclusion scheduler exiting")
			return

		case job, ok := <-w.transactionIncludedQueue:
			if !ok {
				// The job queue is closed, so we can exit.
				log.Info("Transaction inclusion job queue closed, exiting")
				return
			}
			// We have a job to process, so let's get a worker, and send the
			// job to it.

			worker := <-w.transactionIncludedJobQueue
			worker <- job
		}
	}
}

// transactionIncludedResponseHandler is the response handler for the
// transaction inclusion job queue.
//
// It is responsible for determining the outcome of the performed job, and
// rescheduling the job upon failure if necessary.  If a transaction is taking
// too long to be included in Espresso, it will be resubmitted to the
// submit transaction job queue for another attempt.
func (w *MultiWorkerQueueEspressoSubmitter) transactionIncludedResponseHandler(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Info("Transaction inclusion response handler exiting")
			return

		case response, ok := <-w.transactionIncludedResponse:
			if !ok {
				log.Warn("Transaction inclusion response handler channel closed, exiting")
				return
			}

			if response.err != nil {
				if elapsed := time.Since(response.job.submitSuccess); elapsed > w.resubmissionDeadline {
					log.Warn("Transaction not included in Espresso within the deadline", "hash", response.job.txnHash.String(), "commit", common.Hash(response.job.txn.Commit()), "elapsed", elapsed, "attempts", response.job.attempt)
					// We have exceeded the resubmission deadline, so we, need
					// to try and resubmit the transaction.
					w.submitTxnsQueue <- SubmitTransactionJob{
						txn:     response.job.txn,
						attempt: response.job.submitAttempts + 1,
					}
					continue
				}

				log.Info("Transaction not included in Espresso", "hash", response.job.txnHash.String(), "commit", common.Hash(response.job.txn.Commit()), "attempts", response.job.attempt, "error", response.err)

				// Handle error case
				// Requeue the transaction for another inclusion check attempt
				job := response.job
				job.attempt++
				job.lastAttempt = time.Now()
				w.transactionIncludedQueue <- job
				continue
			}

			// Transaction successfully included, we have no further worker to
			// do, so we can explicitly drop the job here.

			log.Info("Transaction successfully included in Espresso", "hash", response.job.txnHash.String(), "commit", common.Hash(response.job.txn.Commit()), "attempts", response.job.attempt)
		}
	}
}
