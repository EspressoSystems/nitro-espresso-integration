package espresso

import (
	"context"

	espresso_client "github.com/EspressoSystems/espresso-network/sdks/go/client"
	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	espresso_common "github.com/EspressoSystems/espresso-network/sdks/go/types/common"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/execution"
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

// TransactionStreamerLightClientReadeInterface defines the interface under
// which the TransactionStreamer interacts with the Espresso Light Client
// Reader.
//
// It derives the method definitions from the espresso_client package:
// "github.com/EspressoSystems/espresso-network/sdks/go/light-client"
//
// It is defined separately here from the espresso_client.LightClientReader
// interface in order to minimize the defined and exposed methods.  This
// allows this interface to be much easier to mock in tests, and to serve
// as explicit documentation of the methods utilized by the TransactionStreamer.
type TransactionStreamerLightClientReadeInterface interface {
	IsHotShotLive(delayThreshold uint64) (bool, error)
}

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
	Reorg(count arbutil.MessageIndex, newMessages []arbostypes.MessageWithMetadataAndBlockInfo, oldMessages []*arbostypes.MessageWithMetadata) ([]*execution.MessageResult, error)
	HeadMessageNumber() (arbutil.MessageIndex, error)
	MarkFeedStart(to arbutil.MessageIndex)
	ResultAtPos(pos arbutil.MessageIndex) (*execution.MessageResult, error)
	DigestMessage(num arbutil.MessageIndex, msg *arbostypes.MessageWithMetadata, msgForPrefetch *arbostypes.MessageWithMetadata) (*execution.MessageResult, error)

	BlockNumberToMessageIndex(blockNum uint64) (arbutil.MessageIndex, error)
}
