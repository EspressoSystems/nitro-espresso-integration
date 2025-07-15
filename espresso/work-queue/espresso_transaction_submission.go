package workqueue

import (
	"strings"
	"time"

	"context"
	"sync"

	espresso_client "github.com/EspressoSystems/espresso-network/sdks/go/client"
	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	espresso_common "github.com/EspressoSystems/espresso-network/sdks/go/types/common"
	"github.com/ethereum/go-ethereum/log"
)

// TransactionStreamerEspressoClient defines the interface under which the
// TransactionStreamer interacts with the Espresso Client.
//
// It derives the method definitions from the espresso_client package:
// "github.com/EspressoSystems/espresso-network/sdks/go/client"
//
// It is defined separately here from the espresso_client.EspressoClient
// interface in order to minimize the defined and exposed methods.  This
/// allows this interface to be much easier to mock in tests, and to serve
// as explicit documentation of the methods utilized by the TransactionStreamer.

type TransactionStreamerEspressoClient interface {
	// Get the transactions belonging to the given namespace at the block height,
	// along with a proof that these are all such transactions.
	FetchTransactionsInBlock(ctx context.Context, blockHeight uint64, namespace uint64) (espresso_client.TransactionsInBlock, error)

	// Get the transaction by its hash.
	FetchTransactionByHash(ctx context.Context, hash *espresso_types.TaggedBase64) (espresso_types.TransactionQueryData, error)

	// Submit a transaction to the espresso sequencer.
	SubmitTransaction(ctx context.Context, tx espresso_common.Transaction) (*espresso_common.TaggedBase64, error)
}

// espressoSubmitTransactionJob is a struct that holds the state required to
// submit a transaction to Espresso.
// It contains the transaction to be submitted itself, and a number to
// track the total number of attempts to submit this transaction to Espresso.
type espressoSubmitTransactionJob struct {
	attempts    int
	transaction *espresso_common.Transaction
}

// espressoSubmitTransactionJobResponse is a struct that holds the
// response from the Espresso client after submitting a transaction.
// It contains the job that was submitted, the hash of the transaction
// that was submitted (if successful), and any error that occurred during the
// submission (if unsuccessful).
type espressoSubmitTransactionJobResponse struct {
	job  espressoSubmitTransactionJob
	hash *espresso_common.TaggedBase64
	err  error
}

// espressoTransactionJobAttempt is a struct that holds the job and
// response channel for a transaction submission job.
//
// This is the unit of work that is submitted to the worker to process
// for transaction submissions.
type espressoTransactionJobAttempt struct {
	job  espressoSubmitTransactionJob
	resp chan espressoSubmitTransactionJobResponse
}

// espressoVerifyReceiptJob is a struct that holds the state required to
// verify a receipt for a transaction that was submitted to Espresso.
// It contains the transaction that was submitted, the hash of the
// transaction, and the number of attempts to verify the receipt.
type espressoVerifyReceiptJob struct {
	attempts    int
	start       time.Time
	transaction espressoSubmitTransactionJob
	hash        *espresso_common.TaggedBase64
}

// espressoVerifyReceiptJobResponse is a struct that holds the
// response from the Espresso client after verifying a receipt.
// It contains the job that was submitted, and any error that occurred
// during the verification (if unsuccessful).
type espressoVerifyReceiptJobResponse struct {
	job espressoVerifyReceiptJob
	err error
}

// espressoVerifyReceiptJobAttempt is a struct that holds the job and
// response channel for a receipt verification job.
//
// This is the unit of work that is submitted to the worker to process
// for receipt verifications.
type espressoVerifyReceiptJobAttempt struct {
	job  espressoVerifyReceiptJob
	resp chan espressoVerifyReceiptJobResponse
}

// espressoTransactionSubmitter is a struct that holds the state that governs
// the worker queue processing details for submitting transactions to Espresso
// without spawning arbitrarily many goroutines.
type espressoTransactionSubmitter struct {
	ctx                      context.Context
	wg                       *sync.WaitGroup
	submitJobQueue           chan espressoSubmitTransactionJob
	submitRespQueue          chan espressoSubmitTransactionJobResponse
	submitWorkerQueue        chan chan espressoTransactionJobAttempt
	verifyReceiptJobQueue    chan espressoVerifyReceiptJob
	verifyReceiptRespQueue   chan espressoVerifyReceiptJobResponse
	verifyReceiptWorkerQueue chan chan espressoVerifyReceiptJobAttempt
	espresso                 TransactionStreamerEspressoClient
}

// EspressoTransactionSubmitterConfig is a configuration struct for the
// EspressoTransactionSubmitter. It contains the configurable details for
// creating the EspressoTransactionSubmitter.
type EspressoTransactionSubmitterConfig struct {
	Ctx                                context.Context
	EspressoClient                     TransactionStreamerEspressoClient
	Wg                                 *sync.WaitGroup
	SubmitJobQueueCapacity             int
	SubmitResponseQueueCapacity        int
	VerifyReceiptJobQueueCapacity      int
	VerifyReceiptResponseQueueCapacity int
}

// EspressoTransactionSubmitterOption is a function that can be used to
// configure the EspressoTransactionSubmitterConfig.
type EspressoTransactionSubmitterOption func(*EspressoTransactionSubmitterConfig)

// WithContext is an option that can be used to set the Espresso client
// for the EspressoTransactionSubmitterConfig.
func WithContext(ctx context.Context) EspressoTransactionSubmitterOption {
	return func(config *EspressoTransactionSubmitterConfig) {
		config.Ctx = ctx
	}
}

// WithEspressoClient is an option that can be used to set the Espresso client
// for the EspressoTransactionSubmitterConfig.
func WithEspressoClient(client TransactionStreamerEspressoClient) EspressoTransactionSubmitterOption {
	return func(config *EspressoTransactionSubmitterConfig) {
		config.EspressoClient = client
	}
}

// WithWaitGroup is an option that can be used to set the wait group
// for the EspressoTransactionSubmitterConfig.
func WithWaitGroup(wg *sync.WaitGroup) EspressoTransactionSubmitterOption {
	return func(config *EspressoTransactionSubmitterConfig) {
		config.Wg = wg
	}
}

// NewEspressoTransactionSubmitter creates a new EspressoTransactionSubmitter
// with the given context and espresso client.  It will create a new transaction
// submitter with some default options, and apply those options to the
// configuration.
//
// The resulting instance should reflect the given configuration.
// After returning, the caller should call SpawnWorkers to start the workers,
// and Start to start the job scheduling and response handling portions of the
// transaction submitter. After that, the user should be able to submit
// transactions to the submitter via the SubmitTransaction method.
func NewEspressoTransactionSubmitter(options ...EspressoTransactionSubmitterOption) *espressoTransactionSubmitter {
	config := EspressoTransactionSubmitterConfig{
		Ctx:                                context.Background(),
		Wg:                                 new(sync.WaitGroup),
		SubmitJobQueueCapacity:             1024,
		SubmitResponseQueueCapacity:        10,
		VerifyReceiptJobQueueCapacity:      1024,
		VerifyReceiptResponseQueueCapacity: 10,
	}

	for _, option := range options {
		option(&config)
	}

	if config.EspressoClient == nil {
		panic("Espresso client is required")
	}

	return &espressoTransactionSubmitter{
		ctx:                      config.Ctx,
		wg:                       config.Wg,
		submitJobQueue:           make(chan espressoSubmitTransactionJob, config.SubmitJobQueueCapacity),
		submitRespQueue:          make(chan espressoSubmitTransactionJobResponse, config.SubmitResponseQueueCapacity),
		submitWorkerQueue:        make(chan chan espressoTransactionJobAttempt),
		verifyReceiptJobQueue:    make(chan espressoVerifyReceiptJob, config.VerifyReceiptJobQueueCapacity),
		verifyReceiptRespQueue:   make(chan espressoVerifyReceiptJobResponse, config.VerifyReceiptResponseQueueCapacity),
		verifyReceiptWorkerQueue: make(chan chan espressoVerifyReceiptJobAttempt),
		espresso:                 config.EspressoClient,
	}

}

// SubmitTransaction will submit a transaction to the Job queue.
//
// NOTE: This submits to a channel, and as a result, if the channel is full,
// it will block execution until the channel is able to accept the job.
// If the channel is buffered with sufficient space, it should not cause
// any blocking issues.
func (s *espressoTransactionSubmitter) SubmitTransaction(job *espresso_common.Transaction) {
	s.submitJobQueue <- espressoSubmitTransactionJob{
		transaction: job,
	}
}

// Evaluation result for a job.
type JobEvaluation int

const (
	// Continue handling the current job.
	Handle JobEvaluation = iota
	// Retry the submission.
	RetrySubmission
	// Retry the verification.
	RetryVerification
	// Skip the current job and proceed to the next one.
	Skip
)

// TODO (Keyao) Update the espresso-network-go repo for better error handling.
// <https://app.asana.com/1/1208976916964769/project/1209392461754458/task/1210405729138484?focus=true>
//
// Evaluate the submission job.
//
// # Returns
//
// * If there is no error: Handle.
//
// * If there is an issue on our side: Skip.
//
// * Otherwise: RetrySubmission.
func evaluateSubmission(jobResp espressoSubmitTransactionJobResponse) JobEvaluation {
	err := jobResp.err

	// If there's no error, continue handling the submission.
	if err == nil {
		return Handle
	}

	msg := err.Error()

	// If the transaction is invalid due to a JSON error, skip the submission.
	if strings.Contains(msg, "json: unsupported type:") ||
		strings.Contains(msg, "json: unsupported value:") ||
		strings.Contains(msg, "json: error calling") ||
		strings.Contains(msg, "json: invalid UTF-8 in string") ||
		strings.Contains(msg, "json: invalid number literal") ||
		strings.Contains(msg, "json: encoding error for type") {
		log.Warn("json.Marshal fails, skipping", "msg", msg)
		return Skip
	}

	// If the request is invalid (likely due to API change), skip the submission.
	if strings.Contains(msg, "net/http: nil Context") ||
		strings.Contains(msg, "net/http: invalid method") ||
		strings.HasPrefix(msg, "parse ") {
		log.Warn("NewRequestWithContext fails, skipping", "msg", msg)
		return Skip
	}

	// Otherwise, retry the submission.
	return RetrySubmission
}

// handleTransactionSubmitJobResponse is a function that is meant to be run in a
// goroutine.
//
// It handles the responses from the submit transaction jobs.  It will
// determine if the transaction was successfully submitted to Espresso, and
// if not, it will retry the transaction.  If the transaction was successfully
// submitted, it will then submit a job to the verify receipt job queue to
// verify the receipt of the transaction.
func (s *espressoTransactionSubmitter) handleTransactionSubmitJobResponse() {
	for {
		var jobResp espressoSubmitTransactionJobResponse
		var ok bool

		select {
		case <-s.ctx.Done():
			return
		case jobResp, ok = <-s.submitRespQueue:
			if !ok {
				// Our channel is closed, and we are done
				return
			}
		}

		switch evaluation := evaluateSubmission(jobResp); evaluation {
		case Skip:
			continue
		case RetrySubmission:
			s.submitJobQueue <- jobResp.job
			continue
		}

		verifyJob := espressoVerifyReceiptJob{
			start:       time.Now(),
			transaction: jobResp.job,
			hash:        jobResp.hash,
		}

		select {
		case <-s.ctx.Done():
			return
		// Move to verifying the receipt
		case s.verifyReceiptJobQueue <- verifyJob:
		}
	}
}

// VERIFY_RECEIPT_TIMEOUT is the amount of time we will wait for a receipt to
// be verified before we requeue the job for another attempt.
const VERIFY_RECEIPT_TIMEOUT = 4 * time.Second

// VERIFY_RECEIPT_RETRY_DELAY is the amount of time we will wait before
// retrying a job that failed to verify the receipt.
const VERIFY_RECEIPT_RETRY_DELAY = 100 * time.Millisecond

// TODO (Keyao) Update the espresso-network-go repo for better error handling.
// <https://app.asana.com/1/1208976916964769/project/1209392461754458/task/1210405729138484?focus=true>
//
// Evaluate the verification job.
//
// # Returns
//
// * If there is no error: Handle.
//
// * If there is an issue on our side: Skip.
//
// * If the verification times out: RetrySubmission.
//
// * Otherwise: RetryVerification.
func evaluateVerification(jobResp espressoVerifyReceiptJobResponse) JobEvaluation {
	err := jobResp.err

	// If there's no error, continue handling the verification.
	if err == nil {
		return Handle
	}

	// If the hash is invalid, skip the verification.
	if strings.Contains(err.Error(), "hash is nil") {
		log.Warn("Hash is nil, skipping")
		return Skip
	}

	// If the verification times out, degrade to the submission phase and try again.
	if have := time.Now(); have.Sub(jobResp.job.start) > VERIFY_RECEIPT_TIMEOUT {
		return RetrySubmission
	}

	// Otherwise, retry the verification.
	return RetryVerification
}

// handleVerifyReceiptJobResponse is a function that is meant to be run in a
// goroutine.
//
// This function handles responses from the verify receipt job queue.  It will
// check the results for any errors, and if there are any errors that are
// applicable to retry, it will requeue the job for another attempt.
// If the the job is successful, no further processing is needed and it is
// considered complete.
// If the job has taken too long to verify, then it will re-submit the job
// back to the submit transaction queue for another attempt.
//
// NOTE: This function currently will loop forever if the transaction is
// never going to be available.
func (s *espressoTransactionSubmitter) handleVerifyReceiptJobResponse() {
	for {
		var jobResp espressoVerifyReceiptJobResponse
		var ok bool

		select {
		case <-s.ctx.Done():
			return
		case jobResp, ok = <-s.verifyReceiptRespQueue:
			if !ok {
				// Our channel is closed, and we are done
				return
			}
		}

		switch evaluation := evaluateVerification(jobResp); evaluation {
		case Skip:
			continue
		case RetrySubmission:
			s.submitJobQueue <- jobResp.job.transaction
			continue
		case RetryVerification:
			s.verifyReceiptJobQueue <- jobResp.job
			continue
		}

		// We're done with this job and transaction, we have successfully
		// confirmed that the transaction was submitted to Espresso
	}
}

// scheduleSubmitTransactionJobs is a function that is meant to be run in a
// goroutine.
//
// It handles the scheduling of submit transaction jobs so that the submit
// transaction workers can process them.
func (s *espressoTransactionSubmitter) scheduleSubmitTransactionJobs() {
	for {
		var ok bool

		// Get a worker from the worker queue
		var worker chan espressoTransactionJobAttempt
		select {
		case <-s.ctx.Done():
			return

		case worker, ok = <-s.submitWorkerQueue:
			if !ok {
				// Our channel is closed, and we are done
				return
			}
		}

		// Get a job from the job queue
		var job espressoSubmitTransactionJob
		select {
		case <-s.ctx.Done():
			return
		case job, ok = <-s.submitJobQueue:
			if !ok {
				// Our channel is closed, and we are done
				return
			}
		}

		// Submit the job to the worker
		select {
		case <-s.ctx.Done():
			return

		case worker <- espressoTransactionJobAttempt{job: job, resp: s.submitRespQueue}:
		}
	}
}

// scheduleVerifyReceiptJobs is a function that is meant to be run in a
// goroutine.
//
// It handles the scheduling of verify receipt jobs so that the verify receipt
// workers can process them.
func (s *espressoTransactionSubmitter) scheduleVerifyReceiptsJobs() {
	for {
		var ok bool

		// Get a worker from the worker queue
		var worker chan espressoVerifyReceiptJobAttempt
		select {
		case <-s.ctx.Done():
			return

		case worker, ok = <-s.verifyReceiptWorkerQueue:
			if !ok {
				// Our channel is closed, and we are done
				return
			}
		}

		// Get a job from the job queue
		var job espressoVerifyReceiptJob
		select {
		case <-s.ctx.Done():
			return
		case job, ok = <-s.verifyReceiptJobQueue:
			if !ok {
				// Our channel is closed, and we are done
				return
			}
		}

		// Submit the job to the worker
		select {
		case <-s.ctx.Done():
			return

		case worker <- espressoVerifyReceiptJobAttempt{job: job, resp: s.verifyReceiptRespQueue}:
		}
	}
}

// espressoSubmitTransactionWorker is a function that is meant to be run as a
// goroutine.  It will create a channel for it's job queue, and submit those to
// the worker queue in order to wait for work.  It will then take that job and
// attempt to submit the transaction contained within to espresso using the
// given espresso client. It will submit the response back to the channel
// contained within the job attempt it received.
//
// It's lifetime is governed by the context passed to it, and it will stop
// processing when that context is cancelled.
//
// NOTE: If the context is cancelled after a job has been received, but before
// it is able to submit the transaction, or report about it's result, the job
// may be lost.
func espressoSubmitTransactionWorker(
	ctx context.Context,
	wg *sync.WaitGroup,
	cli TransactionStreamerEspressoClient,
	workerQueue chan<- chan espressoTransactionJobAttempt,
) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer wg.Done()
	ch := make(chan espressoTransactionJobAttempt)
	defer close(ch)

	for {
		var ok bool
		select {
		case <-ctx.Done():
			return

			// Queue our job queue, asking for work
		case workerQueue <- ch:
		}

		// Wait for a job to run
		var jobAttempt espressoTransactionJobAttempt
		select {
		case <-ctx.Done():
			return
		case jobAttempt, ok = <-ch:
			if !ok {
				// Our channel is closed, and we are done
				return
			}
		}

		// Submit the transaction to Espresso
		hash, err := cli.SubmitTransaction(ctx, *jobAttempt.job.transaction)

		jobAttempt.job.attempts++
		resp := espressoSubmitTransactionJobResponse{
			job:  jobAttempt.job,
			hash: hash,
			err:  err,
		}

		select {
		case <-ctx.Done():
			return

		// Send the response back via the channel in the job attempt struct
		case jobAttempt.resp <- resp:
		}
	}
}

// espressoVerifyTransactionWorker is a function that is meant to be run as a
// goroutine.  It will create a channel for it's job queue, and submit those to
// the worker queue in order to wait for work.  It will then take that job and
// attempt to verify the transaction contained within to espresso using the
// given espresso client. It will submit the response back to the channel
// contained within the job attempt it received.
func espressoVerifyTransactionWorker(
	ctx context.Context,
	wg *sync.WaitGroup,
	cli TransactionStreamerEspressoClient,
	workerQueue chan<- chan espressoVerifyReceiptJobAttempt,
) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer wg.Done()
	ch := make(chan espressoVerifyReceiptJobAttempt)
	defer close(ch)

	for {
		var ok bool
		select {
		case <-ctx.Done():
			return

			// Queue our job queue, asking for work
		case workerQueue <- ch:
		}

		// Wait for a job to run
		var jobAttempt espressoVerifyReceiptJobAttempt
		select {
		case <-ctx.Done():
			return
		case jobAttempt, ok = <-ch:
			if !ok {
				// Our channel is closed, and we are done
				return
			}
		}

		if jobAttempt.job.attempts > 0 {
			// We have already attempted this job, so we will wait a bit
			// NOTE: this prevents this worker from being able to process
			// other jobs while we wait for this delay.
			time.Sleep(VERIFY_RECEIPT_RETRY_DELAY)
		}

		_, err := cli.FetchTransactionByHash(ctx, jobAttempt.job.hash)

		jobAttempt.job.attempts++
		resp := espressoVerifyReceiptJobResponse{
			job: jobAttempt.job,
			err: err,
		}

		select {
		case <-ctx.Done():
			return

		case jobAttempt.resp <- resp:
		}
	}
}

// SpawnWorkers spawns the given number of workers to process the
// submit transaction jobs and verify receipt jobs.
func (s *espressoTransactionSubmitter) SpawnWorkers(numSubmitTransactionWorkers, numVerifyReceiptWorkers int) {
	workersCtx := s.ctx

	for i := 0; i < numSubmitTransactionWorkers; i++ {
		s.wg.Add(1)
		go espressoSubmitTransactionWorker(workersCtx, s.wg, s.espresso, s.submitWorkerQueue)
	}

	for i := 0; i < numVerifyReceiptWorkers; i++ {
		s.wg.Add(1)
		go espressoVerifyTransactionWorker(workersCtx, s.wg, s.espresso, s.verifyReceiptWorkerQueue)
	}
}

// Start starts the job scheduling and response handling for the Espresso
// transaction submitter.
func (s *espressoTransactionSubmitter) Start() {
	// Submit Transaction Jobs
	go s.scheduleSubmitTransactionJobs()
	go s.handleTransactionSubmitJobResponse()

	// Verify Receipt Jobs
	go s.scheduleVerifyReceiptsJobs()
	go s.handleVerifyReceiptJobResponse()
}
