package espresso_test

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	espresso_client "github.com/EspressoSystems/espresso-network/sdks/go/client"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/offchainlabs/nitro/arbnode"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/espresso"
	"github.com/offchainlabs/nitro/espresso/submitter"
	"github.com/offchainlabs/nitro/execution"
	chain "github.com/offchainlabs/nitro/system_tests/espresso/chain"
	execution_engine "github.com/offchainlabs/nitro/system_tests/espresso/execution-engine"
	key_manager "github.com/offchainlabs/nitro/system_tests/espresso/key-manager"
	light_client "github.com/offchainlabs/nitro/system_tests/espresso/light-client"
)

// GeneratedMessage is a struct that holds the generated message data that
// is needed for the TransactionStreamer to process messages.
type GeneratedMessage struct {
	pos           arbutil.MessageIndex
	msgWithMeta   arbostypes.MessageWithMetadata
	msgResult     execution.MessageResult
	blockMetadata common.BlockMetadata
}

// generateMessage creates a new message with metadata and a result.
// It uses the provided index to fill the message data with a unique identifier.
//
// The message is hashed using the provided hasher, and a MessageResult is
// created with the hash of the message.
func generateMessage(
	i arbutil.MessageIndex,
	hasher execution_engine.MessageHasher,
) GeneratedMessage {
	// msgData := make([]byte, 100)
	msgData := make([]byte, 3*1024)
	// We write the index to the message data
	// This can help to identify when debugging issues
	binary.BigEndian.PutUint64(msgData, uint64(i))
	rand.Read(msgData) // Fill msgData with random bytes
	msg := arbostypes.MessageWithMetadataAndBlockInfo{
		MessageWithMeta: arbostypes.MessageWithMetadata{
			Message: &arbostypes.L1IncomingMessage{
				Header: &arbostypes.L1IncomingMessageHeader{
					Kind: arbostypes.L1MessageType_L2Message,
				},
				L2msg: msgData,
			},
		},
	}
	hash := hasher.Hash(&msg.MessageWithMeta)
	msgResult := &execution.MessageResult{
		BlockHash: hash,
	}
	return GeneratedMessage{
		pos:           i,
		msgWithMeta:   msg.MessageWithMeta,
		msgResult:     *msgResult,
		blockMetadata: nil,
	}
}

// generateMessages is a goroutine that generates messages continuously.
// It uses the provided hasher to hash the messages and sends them to the
// channel.
//
// The messages are generated with an incrementing index, starting from 0.
// The goroutine will stop when the context is done.
//
// This function is meant to be called in a separate goroutine.
func generateMessages(
	ctx context.Context,
	hasher execution_engine.MessageHasher,
	ch chan<- GeneratedMessage,
) {
	defer close(ch)
	i := arbutil.MessageIndex(0)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg := generateMessage(i, hasher)
			ch <- msg
			i++
		}
	}
}

// generateNMessages is a function that is meant to run in a separate goroutine.
// It generates up to the specified number of messages and sends them to the
// provided channel.  The index of each message starts at 0 and will increment
// for each message generated.
//
// When the function exits, the provided channel will be closed.
func generateNMessages(
	ctx context.Context,
	hasher execution_engine.MessageHasher,
	ch chan<- GeneratedMessage,
	n int,
) {
	defer close(ch)
	i := arbutil.MessageIndex(0)

	for j := 0; j < n; j++ {
		select {
		case <-ctx.Done():
			return
		default:
			msg := generateMessage(i, hasher)
			ch <- msg
			i++
		}
	}
}

// writeMessagesToSequencerAtInterval is a goroutine that reads messages from
// the provided channel and writes them to the TransactionStreamer at a
// specified interval.
//
// This function is meant to be called in a separate goroutine.
func writeMessagesToSequencerAtInterval(
	ctx context.Context,
	streamer *arbnode.TransactionStreamer,
	ch <-chan GeneratedMessage,
	interval time.Duration,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg, ok := <-ch
			if !ok {
				return
			}

			if have, want := streamer.WriteMessageFromSequencer(
				msg.pos,
				msg.msgWithMeta,
				msg.msgResult,
				msg.blockMetadata,
			), error(nil); have != want {
				panic(fmt.Sprintf(
					"encountered error while writing message from sequencer:\nhave:\n\t\"%v\"\nwant:\n\t\"%v\"",
					have, want,
				))
			}
		}
	}
}

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

	for _, pollingInterval := range []time.Duration{1 * time.Second} {
		for _, submissionInterval := range []time.Duration{5 * time.Second} {
			ctx, cancel := context.WithCancel(ctx)

			// Create arbDB with fragmented blockMetadata across blocks
			arbDb := rawdb.NewMemoryDatabase()
			fatalErrorChan := make(chan error, 1)

			exec := execution_engine.NewMockExecutionEngine(hasher)

			streamer, err := arbnode.NewTransactionStreamer(
				ctx,
				arbDb,
				params.TestChainConfig,
				exec,
				nil,
				fatalErrorChan,
				func() *arbnode.TransactionStreamerConfig {
					return &arbnode.DefaultTransactionStreamerConfig
				},
				nil,
			)

			// Ensure that the TransactionStreamer was created successfully
			if have, want := err, error(nil); have != want {
				t.Fatalf("encountered error while creating TransactionStreamer:\nhave:\n\t\"%v\"\nwant:\n\t\"%v\"", have, want)
			}

			// Create a mock Espresso Chain
			mockEspressoChain := chain.NewMockEspressoChain()
			// Produce Espresso Blocks at a 2 second interval
			go chain.ProduceEspressoBlocksAtInterval(ctx, mockEspressoChain, 2*time.Second)

			// Simulate a client with a delay in message processing (to simulate network delays)
			var espressoClient espresso.TransactionStreamerEspressoClient = mockEspressoChain

			blocksWithTransactionsCh := make(chan espresso_client.TransactionsInBlock, N)
			espressoClient = chain.NewSiphonBlocksWithTransactions(espressoClient, blocksWithTransactionsCh)
			espressoClient = chain.NewEspressoChainDelayed(espressoClient, 250*time.Millisecond, 250*time.Millisecond, 250*time.Millisecond)
			// espressoClient = NewEspressoChainExponentialDelay(espressoClient)
			// espressoClient = chain.NewEspressoChainMetrics(espressoClient)

			// espressoMetrics := espressoClient.(*chain.EspressoChainClientMetrics)

			espressoSubmitter, err := submitter.NewEspressoOriginalSubmitter(
				// Configure the TransactionStreamer's Espresso fields that are unable to
				// be directly set in the NewTransactionStreamer call.
				arbnode.WithTransactionStreamer(streamer),
				submitter.WithEspressoClient(espressoClient),
				submitter.WithLightClientReader(light_client.NewMockAlwaysLiveLightClientReader()),
				submitter.WithKeyManager(key_manager.NewMockEspressoKeyManager()),
				submitter.WithMaxTransactionSize(1_000_000),
				submitter.WithTxnsPollingInterval(pollingInterval),
				submitter.WithTxnsSubmissionInterval(submissionInterval),
				submitter.WithResubmitEspressoTxDeadline(arbnode.DefaultBatchPosterConfig.ResubmitEspressoTxDeadline),
			)
			if have, want := err, error(nil); have != want {
				t.Fatalf("encountered error while creating EspressoOriginalSubmitter:\nhave:\n\t\"%v\"\nwant:\n\t\"%v\"", have, want)
			}

			// Enable Espresso by setting the EspressoSubmitter in the
			// TransactionStreamer.
			arbnode.SetEspressoSubmitter(streamer, espressoSubmitter)

			// Setup a buffer of GeneratedMessages to be sent to the TransactionStreamer
			// so we can build up a backlog of messages to be processed.
			messagesInChannel := make(chan GeneratedMessage, N)

			start := time.Now()
			// Start the TransactionStreamer, so that processing begins
			if have, want := streamer.Start(ctx), error(nil); have != want {
				t.Fatalf("encountered error while starting TransactionStreamer:\nhave:\n\t\"%v\"\nwant:\n\t\"%v\"", have, want)
			}

			// Start sending transactions to the TransactionStreamer at a specified
			// interval. This simulates the sequencer sending messages to the
			// TransactionStreamer.

			// Produce the messages in the channel, so that the TransactionStreamer can
			// process them.
			go generateNMessages(ctx, hasher, messagesInChannel, N)
			go writeMessagesToSequencerAtInterval(ctx, streamer, messagesInChannel, time.Millisecond)

			// Wait for some time to allow for the transactions to be sent to and
			// processed by the mock espresso chain.
			// time.Sleep(time.Second * 60)

			// We need to grab the transactions

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

						hash := hasher.Hash(&messageWithMetadata)
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
