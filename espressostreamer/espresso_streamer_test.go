package espressostreamer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	espressoClient "github.com/EspressoSystems/espresso-network/sdks/go/client"
	"github.com/EspressoSystems/espresso-network/sdks/go/types"
	espressoTypes "github.com/EspressoSystems/espresso-network/sdks/go/types"
	espressoCommon "github.com/EspressoSystems/espresso-network/sdks/go/types/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	legacy_espressogen "github.com/offchainlabs/nitro/espresso-tee-contracts-legacy/espressogen"
)

func TestEspressoStreamer(t *testing.T) {
	t.Run("Peek should not change the current position", func(t *testing.T) {
		ctx := context.Background()
		mockEspressoClient := new(mockEspressoClient)
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)

		streamer := NewEspressoStreamer(1, 3, mockEspressoTEEVerifierClient, mockEspressoClient, false, func(l1Height uint64, addr common.Address) (bool, error) { return false, nil }, 1*time.Second, 0)

		streamer.Reset(1, 3)

		before := streamer.currentMessagePos
		r := streamer.Peek(ctx)
		assert.Nil(t, r)
		assert.Equal(t, before, streamer.currentMessagePos)

		streamer.messageWithMetadataAndPos = []*MessageWithMetadataAndPos{
			{
				MessageWithMeta: arbostypes.MessageWithMetadata{},
				Pos:             1,
				HotshotHeight:   3,
			},
			{
				MessageWithMeta: arbostypes.MessageWithMetadata{},
				Pos:             2,
				HotshotHeight:   4,
			},
		}

		r = streamer.Peek(ctx)
		assert.Equal(t, streamer.messageWithMetadataAndPos[0], r)
		assert.Equal(t, before, streamer.currentMessagePos)
		assert.Equal(t, len(streamer.messageWithMetadataAndPos), 2)
	})
	t.Run("Next should consume a message if it is in buffer", func(t *testing.T) {
		ctx := context.Background()
		mockEspressoClient := new(mockEspressoClient)
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)

		streamer := NewEspressoStreamer(1, 3, mockEspressoTEEVerifierClient, mockEspressoClient, false, func(l1Height uint64, addr common.Address) (bool, error) { return false, nil }, 1*time.Second, 0)

		streamer.Reset(1, 3)

		// Empty buffer. Should not change anything
		initialPos := streamer.currentMessagePos
		r := streamer.Next(ctx)
		assert.Nil(t, r)
		assert.Equal(t, initialPos, streamer.currentMessagePos)

		streamer.messageWithMetadataAndPos = []*MessageWithMetadataAndPos{
			{
				MessageWithMeta: arbostypes.MessageWithMetadata{},
				Pos:             1,
				HotshotHeight:   3,
			},
			{
				MessageWithMeta: arbostypes.MessageWithMetadata{},
				Pos:             2,
				HotshotHeight:   4,
			},
		}

		r = streamer.Next(ctx)
		assert.Equal(t, streamer.messageWithMetadataAndPos[0], r)
		assert.Equal(t, initialPos+1, streamer.currentMessagePos)
		// Buffer should still have 2 messages.
		assert.Equal(t, len(streamer.messageWithMetadataAndPos), 2)

		// Second message
		// Peek would cleanup the outdated messages as well
		peekMessage := streamer.Peek(ctx)
		assert.NotNil(t, peekMessage)
		assert.Equal(t, initialPos+1, streamer.currentMessagePos)
		assert.Equal(t, len(streamer.messageWithMetadataAndPos), 1)

		newMessage := streamer.Next(ctx)
		assert.Equal(t, peekMessage, newMessage)
		assert.Equal(t, initialPos+2, streamer.currentMessagePos)

		// Empty message should not alter the current position
		third := streamer.Next(ctx)
		assert.Nil(t, third)
		assert.Equal(t, initialPos+2, streamer.currentMessagePos)
	})
	t.Run("Test should pop messages in order", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		mockEspressoClient := new(mockEspressoClient)
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)

		// Simulate the call to the tee verifier returning a byte array. To the streamer, this indicates the attestation quote is valid.
		mockEspressoTEEVerifierClient.On("Verify", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
		// create a new streamer object
		streamer := NewEspressoStreamer(1, 1, mockEspressoTEEVerifierClient, mockEspressoClient, false, func(l1Height uint64, addr common.Address) (bool, error) { return false, nil }, 1*time.Second, 1)
		streamer.Reset(735805, 1)
		// Get the data for this test
		testBlocks := GetTestBlocks()

		mockEspressoClient.On("FetchLatestBlockHeight", ctx).Return(testBlocks[0].blockNumber, nil)
		mockEspressoClient.On("FetchTransactionsInBlock", ctx, testBlocks[0].blockNumber, uint64(1)).Return(testBlocks[0].transactionsInBlock, nil)
		// manually crank the streamers polling function to read an individual hotshot block prepared for the mockEspressoClient
		err := streamer.QueueMessagesFromHotshot(ctx, streamer.parseEspressoTransaction)
		require.NoError(t, err)

		shouldStop := false
		logger := make(chan int)
		runner := func() *MessageWithMetadataAndPos {
			count := 0
			for {
				log.Info("entering runner")
				msg := streamer.Next(ctx)
				if msg == nil && count%100000 == 0 {
					log.Info("msg is nil")
					count += 1
					logger <- count
				} else if msg != nil {
					log.Info("msg is non nil", "msg", msg)
					shouldStop = true
					return msg
				}
			}
		}
		loggerFunc := func() {
			for {
				count := <-logger
				log.Info("Cout before message", "count", count)
				if shouldStop {
					return
				}
			}
		}

		go loggerFunc()
		msg := runner()

		// Assert that the streamer believe this message to have originated at hotshot height 1
		assert.Equal(t, msg.HotshotHeight, uint64(1))
	})
	t.Run("Streamer should not skip any hotshot blocks", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		mockEspressoClient := new(mockEspressoClient)
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)

		namespace := uint64(1)
		mockEspressoClient.On("FetchTransactionsInBlock", ctx, uint64(3), namespace).Return(espressoClient.TransactionsInBlock{}, nil)

		mockEspressoClient.On("FetchTransactionsInBlock", ctx, uint64(4), namespace).Return(espressoClient.TransactionsInBlock{}, nil)

		mockEspressoClient.On("FetchTransactionsInBlock", ctx, uint64(5), namespace).Return(espressoClient.TransactionsInBlock{}, nil)

		mockEspressoClient.On("FetchTransactionsInBlock", ctx, uint64(6), namespace).Return(espressoClient.TransactionsInBlock{}, errors.New("test error"))

		streamer := NewEspressoStreamer(namespace, 3, mockEspressoTEEVerifierClient, mockEspressoClient, false, func(l1Height uint64, addr common.Address) (bool, error) { return false, nil }, 1*time.Second, 0)

		testParseFn := func(tx types.Bytes, l1 uint64) ([]*MessageWithMetadataAndPos, error) {
			return nil, nil
		}

		err := streamer.QueueMessagesFromHotshot(ctx, testParseFn)
		require.NoError(t, err)
		require.Equal(t, streamer.nextHotshotBlockNum, uint64(4))

		err = streamer.QueueMessagesFromHotshot(ctx, testParseFn)
		require.NoError(t, err)
		require.Equal(t, streamer.nextHotshotBlockNum, uint64(5))

		err = streamer.QueueMessagesFromHotshot(ctx, testParseFn)
		require.NoError(t, err)
		require.Equal(t, streamer.nextHotshotBlockNum, uint64(6))

		err = streamer.QueueMessagesFromHotshot(ctx, testParseFn)
		require.Error(t, err)
		require.Equal(t, streamer.nextHotshotBlockNum, uint64(6))

	})
	t.Run("Streamer should query hotshot after being reset", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mockEspressoClient := new(mockEspressoClient)
		mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)

		namespace := uint64(1)
		mockEspressoClient.On("FetchTransactionsInBlock", ctx, uint64(3), namespace).Return(espressoClient.TransactionsInBlock{
			Transactions: []types.Bytes{
				[]byte{0x01, 0x02, 0x03, 0x04},
			},
		}, nil)

		mockEspressoClient.On("FetchTransactionsInBlock", ctx, uint64(4), namespace).Return(espressoClient.TransactionsInBlock{
			Transactions: []types.Bytes{
				[]byte{0x01, 0x02, 0x03, 0x04},
			},
		}, nil)

		streamer := NewEspressoStreamer(namespace, 3, mockEspressoTEEVerifierClient, mockEspressoClient, false, func(l1Height uint64, addr common.Address) (bool, error) { return false, nil }, 1*time.Second, 0)

		testParseFn := func(pos uint64, hotshotheight uint64) func(tx types.Bytes, l1Height uint64) ([]*MessageWithMetadataAndPos, error) {

			return func(tx types.Bytes, l1Height uint64) ([]*MessageWithMetadataAndPos, error) {
				return []*MessageWithMetadataAndPos{
					{
						MessageWithMeta: arbostypes.MessageWithMetadata{
							Message: &arbostypes.L1IncomingMessage{},
						},
						Pos:           pos,
						HotshotHeight: hotshotheight,
					},
				}, nil
			}
		}

		err := streamer.QueueMessagesFromHotshot(ctx, testParseFn(3, 3))
		require.NoError(t, err)

		err = streamer.QueueMessagesFromHotshot(ctx, testParseFn(4, 4))
		require.NoError(t, err)

		require.Equal(t, 2, len(streamer.messageWithMetadataAndPos))

		streamer.Reset(0, 3)

		require.Equal(t, 0, len(streamer.messageWithMetadataAndPos))

		err = streamer.QueueMessagesFromHotshot(ctx, testParseFn(3, 3))
		require.NoError(t, err)

		require.Equal(t, len(streamer.messageWithMetadataAndPos), 1)
	})

	t.Run("rpc error should retry", func(t *testing.T) {
		ctx := context.Background()
		mockEspressoClient := new(mockEspressoClient)
		namespace := uint64(1)
		blockNum := uint64(3)

		tx1, tx2, tx3 := espressoTypes.Bytes{0x01}, espressoTypes.Bytes{0x02}, espressoTypes.Bytes{0x03}
		mockEspressoClient.On("FetchTransactionsInBlock", ctx, blockNum, namespace).Return(espressoClient.TransactionsInBlock{
			Transactions: []espressoTypes.Bytes{tx1, tx2, tx3},
		}, nil).Once()

		parseAttemptCount := 0
		parseFn := func(tx types.Bytes, _ uint64) ([]*MessageWithMetadataAndPos, error) {
			if assert.ObjectsAreEqual(tx, tx2) {
				parseAttemptCount++
				return nil, rpc.ErrNoResult
			}
			return []*MessageWithMetadataAndPos{{
				MessageWithMeta: arbostypes.MessageWithMetadata{},
				Pos:             uint64(tx[0]),
				HotshotHeight:   blockNum,
			}}, nil
		}

		messages, _, err := fetchNextHotshotBlock(ctx, mockEspressoClient, blockNum, parseFn, namespace)
		require.NoError(t, err)

		require.Equal(t, 2, len(messages), "Expected to process two messages")
		if len(messages) == 2 && len(tx1) > 0 && len(tx3) > 0 {
			assert.Equal(t, uint64(tx1[0]), messages[0].Pos)
			assert.Equal(t, uint64(tx3[0]), messages[1].Pos)
		}

		require.Equal(t, 1, parseAttemptCount, "Expected the failing transaction to be attempted only once")

		mockEspressoClient.AssertExpectations(t)
	})
}

// This serves to assert that we should be expecting a specific error during the test, and if the error does not match, fail the test.
func ExpectErr(t *testing.T, err error, expectedError error) {
	t.Helper()
	if !errors.Is(err, expectedError) {
		t.Fatal(err, expectedError)
	}
}

// This test ensures that parseEspressoTransaction will have
func TestEspressoEmptyTransaction(t *testing.T) {
	mockEspressoClient := new(mockEspressoClient)
	mockEspressoTEEVerifierClient := new(mockEspressoTEEVerifier)
	streamer := NewEspressoStreamer(1, 1, mockEspressoTEEVerifierClient, mockEspressoClient, false, func(l1Height uint64, addr common.Address) (bool, error) { return false, nil }, time.Millisecond, 0)
	// This determines the contents of the message. For this test the contents of the message needs to be empty (not 0's) to properly test the behavior
	msgFetcher := func(arbutil.MessageIndex) ([]byte, error) {
		return []byte{}, nil
	}
	// create an empty payload
	test := []arbutil.MessageIndex{1, 2}
	payload, _ := arbutil.BuildRawHotShotPayload(test, msgFetcher, 100000) // this value is just a random number to get BuildRawHotShotPayload to return a payload
	// create a fake signature for the payload.
	signerFunc := func([]byte) ([]byte, error) {
		return []byte{1}, nil
	}
	signedPayload, _ := arbutil.SignHotShotPayload(payload, signerFunc)
	_, err := streamer.parseEspressoTransaction(signedPayload, 1)
	ExpectErr(t, err, ErrPayloadHadNoMessages)
}

type TestBlock struct {
	blockNumber         uint64
	transactionsInBlock espressoClient.TransactionsInBlock
}

type mockEspressoTEEVerifier struct {
	mock.Mock
}

func (v *mockEspressoTEEVerifier) Verify(opts *bind.CallOpts, attestation []byte, signature [32]byte) (legacy_espressogen.EnclaveReport, error) {
	return legacy_espressogen.EnclaveReport{}, nil
}

type mockEspressoClient struct {
	mock.Mock
}

// StreamTransactions implements client.EspressoClient.
func (m *mockEspressoClient) StreamTransactions(ctx context.Context, height uint64) (espressoClient.Stream[espressoTypes.TransactionQueryData], error) {
	panic("unimplemented")
}

// StreamTransactionsInNamespace implements client.EspressoClient.
func (m *mockEspressoClient) StreamTransactionsInNamespace(ctx context.Context, height uint64, namespace uint64) (espressoClient.Stream[espressoTypes.TransactionQueryData], error) {
	panic("unimplemented")
}

func (m *mockEspressoClient) FetchLatestBlockHeight(ctx context.Context) (uint64, error) {
	args := m.Called(ctx)
	//nolint:errcheck
	return args.Get(0).(uint64), args.Error(1)
}

func (m *mockEspressoClient) FetchExplorerTransactionByHash(ctx context.Context, hash *types.TaggedBase64) (types.ExplorerTransactionQueryData, error) {
	args := m.Called(ctx, hash)
	//nolint:errcheck
	return args.Get(0).(types.ExplorerTransactionQueryData), args.Error(1)
}

// FetchNamespaceTransactionsInRange implements client.EspressoClient.
func (m *mockEspressoClient) FetchNamespaceTransactionsInRange(ctx context.Context, fromHeight uint64, toHeight uint64, namespace uint64) ([]espressoTypes.NamespaceTransactionsRangeData, error) {
	args := m.Called(ctx, namespace, fromHeight, toHeight)
	//nolint:errcheck
	return args.Get(0).([]espressoTypes.NamespaceTransactionsRangeData), args.Error(1)
}

func (m *mockEspressoClient) FetchTransactionsInBlock(ctx context.Context, blockHeight uint64, namespace uint64) (espressoClient.TransactionsInBlock, error) {
	args := m.Called(ctx, blockHeight, namespace)
	//nolint:errcheck
	return args.Get(0).(espressoClient.TransactionsInBlock), args.Error(1)
}

func (m *mockEspressoClient) FetchHeaderByHeight(ctx context.Context, blockHeight uint64) (espressoTypes.HeaderImpl, error) {
	header := espressoTypes.Header0_3{Height: blockHeight, L1Finalized: &espressoTypes.L1BlockInfo{Number: 1}}
	return espressoTypes.HeaderImpl{Header: &header}, nil
}

func (m *mockEspressoClient) FetchHeadersByRange(ctx context.Context, from uint64, until uint64) ([]types.HeaderImpl, error) {
	panic("not implemented")
}

func (m *mockEspressoClient) FetchRawHeaderByHeight(ctx context.Context, height uint64) (json.RawMessage, error) {
	panic("not implemented")
}

func (m *mockEspressoClient) FetchTransactionByHash(ctx context.Context, hash *types.TaggedBase64) (types.TransactionQueryData, error) {
	panic("not implemented")
}

func (m *mockEspressoClient) FetchVidCommonByHeight(ctx context.Context, blockHeight uint64) (types.VidCommon, error) {
	panic("not implemented")
}

func (m *mockEspressoClient) SubmitTransaction(ctx context.Context, tx espressoCommon.Transaction) (*espressoCommon.TaggedBase64, error) {
	panic("not implemented")
}

// To generate test scripts for the mock clients, we can create a list of test blocks that we can iterate through and set as the call and return values.
func GetTestBlocks() []TestBlock {
	var data []TestBlock
	hexVal := "000000000000004169c45efca0139e2bbff863ad7d69401033acd20b4bdb824f661c805affdcb0824dd54cb426272372dedb714250a3fe130fdfbe964e4a163a653522550e70130f010000000001e91ac70000000000001a2cf91a29f91a23e20394a4b000000000000000000073657175656e636572840170d15284695c1e1bc080b919fd0402f919f88281738303c031851c68eaf180851c68eaf180831d4c51942be5d7058adba14bc38e4a83e94a81f7491b016380b9198488be5e4f000000000000000000000000000000000000000000000000000000000000004000000000000000000000000000000000000000000000000000000000000005600000000000000000000000000000000000000000000000000000000000000028000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000110000000000000000000000000000000000000000000000000000000000000026000000000000000000000000000000000000000000000000000000000000006c000000000000000000000000000000000000000000000000000000000000006d000000000000000000000000000000000000000000000000000000000000008100000000000000000000000000000000000000000000000000000000000000830000000000000000000000000000000000000000000000000000000000000085000000000000000000000000000000000000000000000000000000000000008900000000000000000000000000000000000000000000000000000000000000950000000000000000000000000000000000000000000000000000000000000099000000000000000000000000000000000000000000000000000000000000009f00000000000000000000000000000000000000000000000000000000000000ac00000000000000000000000000000000000000000000000000000000000000bf00000000000000000000000000000000000000000000000000000000000000d300000000000000000000000000000000000000000000000000000000000000d400000000000000000000000000000000000000000000000000000000000000db00000000000000000000000000000000000000000000000000000000000000e200000000000000000000000000000000000000000000000000000000000000e900000000000000000000000000000000000000000000000000000000000000f000000000000000000000000000000000000000000000000000000000000000f8000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000001010000000000000000000000000000000000000000000000000000000000000104000000000000000000000000000000000000000000000000000000000000011a000000000000000000000000000000000000000000000000000000000000012e000000000000000000000000000000000000000000000000000000000000013f00000000000000000000000000000000000000000000000000000000000001410000000000000000000000000000000000000000000000000000000000000144000000000000000000000000000000000000000000000000000000000000014c0000000000000000000000000000000000000000000000000000000000000164000000000000000000000000000000000000000000000000000000000000016f00000000000000000000000000000000000000000000000000000000000001780000000000000000000000000000000000000000000000000000000000000179000000000000000000000000000000000000000000000000000000000000017b0000000000000000000000000000000000000000000000000000000000000180000000000000000000000000000000000000000000000000000000000000018100000000000000000000000000000000000000000000000000000000000001820000000000000000000000000000000000000000000000000000000000000185000000000000000000000000000000000000000000000000000000000000018900000000000000000000000000000000000000000000000000000000000000280352030d02c5027b022d01d801800166013f011700ec00be008b00500003402b27101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b005000035b0127101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c802cb0288024001f901b101640116010100e100c0009e007b0056002e00295e3c27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e001fb6a227101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba03440352030d02c5027b022d01d801800166013f011700ec00be008b00500007355f27101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b0050000eac6d27101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b00500000e6e627101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b00500001121a27101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c802cb0288024001f901b101640116010100e100c0009e007b0056002e00116ba627101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e001395fc27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e001192fc27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e0017be1b27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba03440352030d02c5027b022d01d801800166013f011700ec00be008b0050000de34627101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b00500014e59227101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b00500003f62e27101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b00500003fe2627101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b00500018d92227101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b0050001c18cb27101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c802cb0288024001f901b101640116010100e100c0009e007b0056002e0002bc7d27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e0005702127101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e0039fb7027101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e0035e64d27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e000a97b827101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e000b4fd227101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba03440352030d02c5027b022d01d801800166013f011700ec00be008b00500000b6c627101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b00500001380327101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c802cb0288024001f901b101640116010100e100c0009e007b0056002e0006638227101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e000aa59127101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba03440352030d02c5027b022d01d801800166013f011700ec00be008b00500004807127101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b0050000982c927101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c802cb0288024001f901b101640116010100e100c0009e007b0056002e001083bc27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e0013bb9f27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e002a86ae27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e00330b8027101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034403b50372032902df028f023601d701bb01900162013100fb00bd00710002ecfc27101f4017700fa00dac0bb809c407d0075306d0064605b4055804f80493042703b50372032902df028f023601d701bb01900162013100fb00bd00710003d5b327101f4017700fa00dac0bb809c407d0075306d0064605b4055804f8049304270352030d02c5027b022d01d801800166013f011700ec00be008b00500000dcfa27101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b005000028dd927101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c803b50372032902df028f023601d701bb01900162013100fb00bd00710000b93227101f4017700fa00dac0bb809c407d0075306d0064605b4055804f80493042703b50372032902df028f023601d701bb01900162013100fb00bd007100008c8227101f4017700fa00dac0bb809c407d0075306d0064605b4055804f80493042703b50372032902df028f023601d701bb01900162013100fb00bd007100019b0e27101f4017700fa00dac0bb809c407d0075306d0064605b4055804f80493042703b50372032902df028f023601d701bb01900162013100fb00bd0071000210c527101f4017700fa00dac0bb809c407d0075306d0064605b4055804f8049304270352030d02c5027b022d01d801800166013f011700ec00be008b005000022dd027101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b00500003671327101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b00500002f5f327101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b00500003a65127101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c802cb0288024001f901b101640116010100e100c0009e007b0056002e0002d3e827101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e0003f00227101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e000a2d3227101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e000f690527101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba03440352030d02c5027b022d01d801800166013f011700ec00be008b00500003a10627101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b005000051f0e27101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c803b50372032902df028f023601d701bb01900162013100fb00bd007100006cca27101f4017700fa00dac0bb809c407d0075306d0064605b4055804f80493042703b50372032902df028f023601d701bb01900162013100fb00bd00710000782d27101f4017700fa00dac0bb809c407d0075306d0064605b4055804f80493042702cb0288024001f901b101640116010100e100c0009e007b0056002e0003606f27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e00043aa327101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e0002f6f227101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e0004d95e27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e0005864c27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e0005818827101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e0003a94927101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e0004b23f27101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e00051ef527101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e00098fc627101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e002dc6c027101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e002dc6c027101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba03440258021601d20190014e010a00c800b6009c00820068004e0034001a002dc6c027101f4017700fa00dac0bb809c407d007080640057804b0043803c0034802d00258021601d20190014e010a00c800b6009c00820068004e0034001a002dc6c027101f4017700fa00dac0bb809c407d007080640057804b0043803c0034802d00258021601d20190014e010a00c800b6009c00820068004e0034001a002dc6c027101f4017700fa00dac0bb809c407d007080640057804b0043803c0034802d00258021601d20190014e010a00c800b6009c00820068004e0034001a002dc6c027101f4017700fa00dac0bb809c407d007080640057804b0043803c0034802d002cb0288024001f901b101640116010100e100c0009e007b0056002e00003a9827101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e00003a9827101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e00003a9827101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e00003a9827101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e002625a027101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba034402cb0288024001f901b101640116010100e100c0009e007b0056002e002625a027101f4017700fa00dac0bb809c407d00724067605c4050e049e042d03ba03440352030d02c5027b022d01d801800166013f011700ec00be008b005000015c5227101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80352030d02c5027b022d01d801800166013f011700ec00be008b00500003c9e227101f4017700fa00dac0bb809c407d0074006ac06120571050c04a4043803c80258021601d20190014e010a00c800b6009c00820068004e0034001a001ab3f027101f4017700fa00dac0bb809c407d007080640057804b0043803c0034802d00258021601d20190014e010a00c800b6009c00820068004e0034001a001ab3f027101f4017700fa00dac0bb809c407d007080640057804b0043803c0034802d0c001a0c516117c93dbc13d1b667b4bd36a886d3e875b3868b7d8d6c55037b5c3c87216a076a8bf0f2ebede246dc4f4878f13c9d409a294072ead1b70b872f3b751a59b298204c3"
	transactionBytes, err := hex.DecodeString(hexVal)
	if err != nil {
		log.Crit("Failed to decode hex string", "err", err)
	}

	data = append(data, TestBlock{
		blockNumber: 1,
		transactionsInBlock: espressoClient.TransactionsInBlock{
			Transactions: []types.Bytes{transactionBytes},
		},
	})
	return data
}
