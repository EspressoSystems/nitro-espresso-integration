package executionengine

import (
	"crypto"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/offchainlabs/nitro/arbnode"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/util/containers"
)

// MockExecutionEngine is a mock implementation of the execution.ExecutionSequencer
// interface.
//
// It implements a minimal set of the methods required.
// It's current implementation is focused on targeting the methods required
// for testing the TransactionStreamer and EspressoChain functionality.
//
// NOTE: This mock *should* be safe to use between threads.
type MockExecutionEngine struct {
	Lock    sync.RWMutex
	Latest  arbutil.MessageIndex
	Results map[arbutil.MessageIndex]*execution.MessageResult
	Hasher  MessageHasher
}

type MessageHasher interface {
	Hash(msg *arbostypes.MessageWithMetadata) common.Hash
}

type sha256MessageHasher struct{}

func (sha256MessageHasher) Hash(msg *arbostypes.MessageWithMetadata) common.Hash {
	hasher := crypto.SHA256.New()
	hasher.Write([]byte{msg.Message.Header.Kind})
	hasher.Write(msg.Message.L2msg)
	var hash common.Hash
	copy(hash[:], hasher.Sum(nil)[:common.HashLength])
	return hash
}

var DefaultMessageHasher MessageHasher = sha256MessageHasher{}

var _ arbnode.TransactionStreamerExecutionSequencer = &MockExecutionEngine{}

// NewMockExecutionEngine returns an implementation of
// execution.ExecutionSequencer that is implemented by
// MockExecutionEngine.
func NewMockExecutionEngine(hasher MessageHasher) arbnode.TransactionStreamerExecutionSequencer {
	return &MockExecutionEngine{
		Results: make(map[arbutil.MessageIndex]*execution.MessageResult),
		Hasher:  hasher,
	}
}

// BlockNumberToMessageIndex implements execution.ExecutionSequencer.
func (m *MockExecutionEngine) BlockNumberToMessageIndex(blockNum uint64) containers.PromiseInterface[arbutil.MessageIndex] {
	panic("unimplemented")
}

// DigestMessage implements execution.ExecutionSequencer.
func (m *MockExecutionEngine) DigestMessage(msgIdx arbutil.MessageIndex, msg *arbostypes.MessageWithMetadata, msgForPrefetch *arbostypes.MessageWithMetadata) containers.PromiseInterface[*execution.MessageResult] {
	m.Lock.Lock()
	defer m.Lock.Unlock()
	hash := m.Hasher.Hash(msg)
	var blockHash common.Hash
	copy(blockHash[:], hash[:])
	result := execution.MessageResult{
		BlockHash: blockHash,
	}

	m.Results[msgIdx] = &result

	return containers.NewReadyPromise(&result, nil)
}

// HeadMessageIndex implements execution.ExecutionSequencer.
func (m *MockExecutionEngine) HeadMessageIndex() containers.PromiseInterface[arbutil.MessageIndex] {
	m.Lock.RLock()
	defer m.Lock.RUnlock()
	return containers.NewReadyPromise(m.Latest, nil)
}

// MarkFeedStart implements execution.ExecutionSequencer.
func (m *MockExecutionEngine) MarkFeedStart(to arbutil.MessageIndex) containers.PromiseInterface[struct{}] {
	return containers.NewReadyPromise(struct{}{}, nil)
}

// Reorg implements execution.ExecutionSequencer.
func (m *MockExecutionEngine) Reorg(msgIdxOfFirstMsgToAdd arbutil.MessageIndex, newMessages []arbostypes.MessageWithMetadataAndBlockInfo, oldMessages []*arbostypes.MessageWithMetadata) containers.PromiseInterface[[]*execution.MessageResult] {
	panic("unimplemented")
}

// ResultAtMessageIndex implements execution.ExecutionSequencer.
func (m *MockExecutionEngine) ResultAtMessageIndex(msgIdx arbutil.MessageIndex) containers.PromiseInterface[*execution.MessageResult] {
	m.Lock.RLock()
	defer m.Lock.RUnlock()
	result, resultOk := m.Results[msgIdx]
	if !resultOk {
		return containers.NewReadyPromise[*execution.MessageResult](nil, fmt.Errorf("no result found at position %d", msgIdx))
	}

	return containers.NewReadyPromise(result, nil)
}
