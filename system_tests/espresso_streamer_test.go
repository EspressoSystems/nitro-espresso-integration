package arbtest

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	espressoClient "github.com/EspressoSystems/espresso-sequencer-go/client"
	types "github.com/EspressoSystems/espresso-sequencer-go/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/offchainlabs/nitro/espressostreamer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)
 
type TestBlock struct{
  blockNumber uint64
  transactionsInBlock espressoClient.TransactionsInBlock
}

type mockParentChainClient struct{
  mock.Mock
}
// This function serves as a simple pass through for whatever is supplied to the .Return calls given to the mock object.
// for now this will be a 1 byte long array containing a 1 to simulate a valid call to the TEE quote verifier
func (m *mockParentChainClient) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error){
  args := m.Called(ctx, msg, blockNumber)
  return args.Get(0).([]byte), args.Error(1)
}

type mockEspressoClient struct{
  mock.Mock
}

func (m *mockEspressoClient) FetchLatestBlockHeight(ctx context.Context) (uint64, error){
  args := m.Called(ctx)
  return args.Get(0).(uint64), args.Error(1)
}

func (m *mockEspressoClient) FetchTransactionsInBlock(ctx context.Context, blockHeight uint64, namespace uint64) (espressoClient.TransactionsInBlock, error){
  args := m.Called(ctx, blockHeight, namespace)
  return args.Get(0).(espressoClient.TransactionsInBlock), args.Error(1)
}

// To generate test scripts for the mock clients, we can create a list of test blocks that we can iterate through and set as the call and return values.
func ShouldPopMessagesInOrderData() []TestBlock{
  var data []TestBlock
  data = append(data, TestBlock{
    blockNumber: 1,
    transactionsInBlock: espressoClient.TransactionsInBlock{
      Transactions: []types.Bytes{[]byte("AAAAAAAAAAAAAAAAAAAAMgAAAAAAAACa+Jj4leEDlKSwAAAAAAAAAAAAc2VxdWVuY2Vyg3XyA4Rns7MPwIC4cQQC+G2DFelFFYQ7msoAhDuaygCCXcCUTy8jIUoPRR0IA3NlsUHCg8wOqkEBgMABoGurpK2IEPMyYCSuQ/6ELJszaCa8hl24tUmKtYKUYh8UoBA8GxdzJe2dgJgbOS71GNl/axDXlOTlgteZY7vmBp7eHQ==")},
      Proof: json.RawMessage(`{
    "ns_index": [
      0,
      0,
      0,
      0
    ],
    "ns_payload": "AQAAALIAAAAAAAAAAAAAAAAAAAAAAAAyAAAAAAAAAJr4mPiV4QOUpLAAAAAAAAAAAABzZXF1ZW5jZXKDdfIDhGezsw/AgLhxBAL4bYMV6UUVhDuaygCEO5rKAIJdwJRPLyMhSg9FHQgDc2WxQcKDzA6qQQGAwAGga6ukrYgQ8zJgJK5D/oQsmzNoJryGXbi1SYq1gpRiHxSgEDwbF3Ml7Z2AmBs5LvUY2X9rENeU5OWC15lju+YGnt4d",
    "ns_proof": {
      "prefix_elems": "FIELD~AAAAAAAAAAD7",
      "suffix_elems": "FIELD~AAAAAAAAAAD7",
      "prefix_bytes": [],
      "suffix_bytes": []
    }
  }`),
    VidCommon: json.RawMessage(`{
    "poly_commits": "FIELD~AQAAAAAAAAAJ91Pem9J24SDlOM2p1cm696Y7rBsAM0Z8NtlTdN-oqE0",
    "all_evals_digest": "FIELD~5B-cf-SEqvSqrmPsKQvDMI4LXX41XgdSyX2CBWrgX-h7",
    "payload_byte_len": 186,
    "num_storage_nodes": 100,
    "multiplicity": 1
  }`),    
   },   
 })
  return data
}


func TestShouldPopMessagesInOrder(t *testing.T){
  ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

  mockParentChainClient := new(mockParentChainClient)
  mockEspressoClient := new(mockEspressoClient)
  
  //simulate the call to the tee verifier returning a byte array. To the streamer, this indicates the attestation quote is valid.
  mockParentChainClient.On("CallContract").Return([]byte{1})
  // create a new streamer object
  streamer := espressostreamer.NewEspressoStreamer(1, []string{""}, 1, time.Millisecond, time.Millisecond, common.Address{}, new(mockParentChainClient), new(mockEspressoClient))
  // Get the data for this test 
  testBlocks := ShouldPopMessagesInOrderData()
  
  mockEspressoClient.On("FetchLatestBlockHeight", ctx).Return(testBlocks[0].blockNumber)
  mockEspressoClient.On("FetchTransactionsInBlock", ctx, testBlocks[0].blockNumber, 1)
  // manually crank the streamers polling function to read an individual hotshot block prepared for the mockEspressoClient
  err := streamer.QueueMessagesFromHotshot(ctx)
  Require(t, err)

  msg, err := streamer.Next()
  //assert we did not have an error on next
  Require(t, err)
  //assert that the streamer believe this message to have originated at hotshot height 1
  assert.Equal(t, msg.HotshotHeight, 1)

}
