package arbnode

import (
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/util/containers"
)

// TransactionStreamerExecutionSequencer defines the interface under which
// the TransactionStreamer interacts with the ExecutionSequencer interface.
//
// It derives the method definitions from the execution package:
// "github.com/offchainlabs/nitro/execution"
//
// It is defined separately here from the execution.ExecutionSequencer
// interface in order to minimize the defined and exposed methods.  This
// allows this interface to be much easier to mock in tests, and to serve
// as explicit documentation of the methods utilized by the TransactionStreamer.
type TransactionStreamerExecutionSequencer interface {
	Reorg(msgIdxOfFirstMsgToAdd arbutil.MessageIndex, newMessages []arbostypes.MessageWithMetadataAndBlockInfo, oldMessages []*arbostypes.MessageWithMetadata) containers.PromiseInterface[[]*execution.MessageResult]
	HeadMessageIndex() containers.PromiseInterface[arbutil.MessageIndex]
	MarkFeedStart(to arbutil.MessageIndex) containers.PromiseInterface[struct{}]
	ResultAtMessageIndex(msgIdx arbutil.MessageIndex) containers.PromiseInterface[*execution.MessageResult]
	DigestMessage(msgIdx arbutil.MessageIndex, msg *arbostypes.MessageWithMetadata, msgForPrefetch *arbostypes.MessageWithMetadata) containers.PromiseInterface[*execution.MessageResult]

	BlockNumberToMessageIndex(blockNum uint64) containers.PromiseInterface[arbutil.MessageIndex]
}

var _ TransactionStreamerExecutionSequencer = execution.ExecutionClient(nil)
