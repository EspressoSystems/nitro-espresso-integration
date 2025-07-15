package espresso_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	espresso_client "github.com/EspressoSystems/espresso-network/sdks/go/client"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/espresso"
	"github.com/offchainlabs/nitro/espresso/submitter"
	chain "github.com/offchainlabs/nitro/system_tests/espresso/chain"
	execution_engine "github.com/offchainlabs/nitro/system_tests/espresso/execution-engine"
	generate "github.com/offchainlabs/nitro/system_tests/espresso/generate"
	transaction_streamer "github.com/offchainlabs/nitro/system_tests/espresso/transaction-streamer"
)

// TestEspressoPerformance is a test that simulates a simplified interaction
// between the TransactionStreamer and the Espresso chain.
//
// It aims to determine and calculate the effect on throughput that are
// being imposed by the locking strategy that is currently being utilized in
// the three processing loops.
func TestEspressoPerformance(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Generate N Messages, to see how long it takes to process them all.
	const N = 10_000

	type intervals struct {
		pollingInterval    time.Duration
		submissionInterval time.Duration
	}
	measuredTimes := map[intervals]chain.TimingData{}
	hasher := execution_engine.DefaultMessageHasher

	{
		// Setup configurable constants for the test
		const pollingInterval = 1 * time.Second
		const submissionInterval = 5 * time.Second
		const maxTransactionSize = 1_000_000

		ctx, cancel := context.WithCancel(ctx)

		// Setup a buffer of GeneratedMessages to be sent to the TransactionStreamer
		// so we can build up a backlog of messages to be processed.
		messagesInChannel := make(chan generate.Message, N)

		blocksWithTransactionsCh := make(chan espresso_client.TransactionsInBlock, N)

		// Setup the Testing Environment
		mockEspressoChain, _, _, streamer, err := transaction_streamer.NewMockTransactionStreamerEnvironment(
			ctx,
			transaction_streamer.AddEspressoClientOptions(func(espressoClient espresso.TransactionStreamerEspressoClient) espresso.TransactionStreamerEspressoClient {
				return chain.NewSiphonBlocksWithTransactions(espressoClient, blocksWithTransactionsCh)
			}),
			transaction_streamer.AddSubmitterOptions(
				submitter.WithMaxTransactionSize(maxTransactionSize),
				submitter.WithTxnsPollingInterval(pollingInterval),
				submitter.WithTxnsSubmissionInterval(submissionInterval),
			),
		)

		if have, want := err, error(nil); have != want {
			t.Fatalf("encountered error while creating mock transaction streamer environment:\nhave:\n\t\"%v\"\nwant:\n\t\"%v\"", have, want)
		}

		// Produce Espresso Blocks at a 2 second interval
		go chain.ProduceEspressoBlocksAtInterval(ctx, mockEspressoChain, 2*time.Second)

		start := time.Now()
		// Start the TransactionStreamer, so that processing begins
		if have, want := streamer.Start(ctx), error(nil); have != want {
			t.Fatalf("encountered error while starting TransactionStreamer:\nhave:\n\t\"%v\"\nwant:\n\t\"%v\"", have, want)
		}

		// Produce the messages in the channel, so that the TransactionStreamer can
		// process them.
		go generate.GenerateNMessages(
			ctx,
			generate.NewSimpleGenerator(execution_engine.DefaultMessageHasher, 3*1024),
			messagesInChannel,
			N,
		)

		// Start sending transactions to the TransactionStreamer at a specified
		// interval. This simulates the sequencer sending messages to the
		// TransactionStreamer.
		go generate.WriteMessagesToSequencerAtInterval(ctx, streamer, messagesInChannel, time.Millisecond)

		// Let's grab the messages that are being sent to the TransactionStreamer
		// and are being processed by the Espresso chain.
		receivedMessages := make(map[common.Hash]arbostypes.MessageWithMetadata)

		// Let's consume the transactions from the mock espresso chain, until we get all
		// of the transactions that we sent to the TransactionStreamer.
		for len(receivedMessages) < N {
			// Read the next transaction
			blockWithTx := <-blocksWithTransactionsCh

			for _, tx := range blockWithTx.Transactions {
				// We can parse the transactions to get the messages
				// This is a mock function that simulates the parsing of the transaction
				// In a real scenario, this would be replaced with the actual parsing logic
				_, _, _, messages, err := arbutil.ParseHotShotPayload(tx)
				if have, want := err, error(nil); have != want {
					t.Fatalf("encountered error while parsing transaction:\nhave:\n\t\"%v\"\nwant:\n\t\"%v\"", have, want)
				}

				for _, message := range messages {
					var messageWithMetadata arbostypes.MessageWithMetadata
					if have, want := rlp.DecodeBytes(message, &messageWithMetadata), error(nil); have != want {
						t.Fatalf("encountered error while decoding message:\nhave:\n\t\"%v\"\nwant:\n\t\"%v\"", have, want)
					}

					hash := hasher.HashMessageWithMetadata(&messageWithMetadata)
					receivedMessages[hash] = messageWithMetadata
					// Alright, we have received a message from the mock espresso chain.
				}
			}
		}

		// Stop running the TransactionStreamer
		cancel()
		streamer.StopWaiter.StopAndWait()

		end := time.Now()
		timingData := chain.Timing(start, end)

		measuredTimes[intervals{
			pollingInterval:    pollingInterval,
			submissionInterval: submissionInterval,
		}] = timingData
	}

	for intervals, timingData := range measuredTimes {
		fmt.Printf("Polling Interval %s, Submission Interval %s => Took %s, Throughput: %.2f messages/s\n",
			intervals.pollingInterval,
			intervals.submissionInterval,
			timingData.Duration,
			float64(N)/timingData.Duration.Seconds(),
		)
	}
}
