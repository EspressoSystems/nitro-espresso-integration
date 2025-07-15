package generate_messages

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/offchainlabs/nitro/arbnode"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/execution"
	execution_engine "github.com/offchainlabs/nitro/system_tests/espresso/execution-engine"
)

// Message is a struct that holds the generated message data that
// is needed for the TransactionStreamer to process messages.
type Message struct {
	pos           arbutil.MessageIndex
	msgWithMeta   arbostypes.MessageWithMetadata
	msgResult     execution.MessageResult
	blockMetadata common.BlockMetadata
}

// MessageGenerator is an interface that provides a simple way to generate
// random messages for testing purposes.
type MessageGenerator interface {
	GenerateMessage(arbutil.MessageIndex) Message
}

// simpleGenerator is a struct that implements the MessageGenerator
// interface in a very simple way.
//
// It is able to generate messages with a fixed size and a random payload.
// The size of the messages can be configured when creating the generator.
type simpleGenerator struct {
	hasher execution_engine.MessageHasher
	size   int
}

var _ MessageGenerator = simpleGenerator{}

// NewSimpleGenerator creates a new instance of simpleGenerator.
// It takes a hasher and a size as parameters.
func NewSimpleGenerator(hasher execution_engine.MessageHasher, size int) MessageGenerator {
	return simpleGenerator{
		hasher: hasher,
		size:   size,
	}
}

// GenerateMessage implements the MessageGenerator interface.
func (g simpleGenerator) GenerateMessage(
	i arbutil.MessageIndex,
) Message {
	msgData := make([]byte, g.size)
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
	hash := g.hasher.HashMessageWithMetadata(&msg.MessageWithMeta)
	msgResult := &execution.MessageResult{
		BlockHash: hash,
	}
	return Message{
		pos:           i,
		msgWithMeta:   msg.MessageWithMeta,
		msgResult:     *msgResult,
		blockMetadata: nil,
	}
}

// GenerateMessages is a function that is meant to be run in a separate
// goroutine.
// Once launched, it will continuously generate messages and send them
// to the provided channel until the context is done.
//
// The messages are generated with an incrementing index, starting from 0.
// The goroutine will stop when the context is done.
func GenerateMessages(
	ctx context.Context,
	generator MessageGenerator,
	ch chan<- Message,
) {
	defer close(ch)
	i := arbutil.MessageIndex(0)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg := generator.GenerateMessage(i)
			ch <- msg
			i++
		}
	}
}

// GenerateNMessages is a function that is meant to run in a separate goroutine.
// It generates up to the specified number of messages and sends them to the
// provided channel.  The index of each message starts at 0 and will increment
// for each message generated.
//
// When the function exits, the provided channel will be closed.
func GenerateNMessages(
	ctx context.Context,
	generator MessageGenerator,
	ch chan<- Message,
	n int,
) {
	defer close(ch)
	i := arbutil.MessageIndex(0)

	for j := 0; j < n; j++ {
		select {
		case <-ctx.Done():
			return
		default:
			msg := generator.GenerateMessage(i)
			ch <- msg
			i++
		}
	}
}

// WriteMessagesToSequencerAtInterval is a function that is meant to be run in
// a separate goroutine.
//
// At the specified interval, it reads messages from the provided channel
// and writes them to the TransactionStreamer.
func WriteMessagesToSequencerAtInterval(
	ctx context.Context,
	streamer *arbnode.TransactionStreamer,
	ch <-chan Message,
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
