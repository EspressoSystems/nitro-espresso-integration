package espressostreamer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/EspressoSystems/espresso-network/sdks/go/client"
	espressoClient "github.com/EspressoSystems/espresso-network/sdks/go/client"
	"github.com/EspressoSystems/espresso-network/sdks/go/types"
	espressoTypes "github.com/EspressoSystems/espresso-network/sdks/go/types"
	espressoCommon "github.com/EspressoSystems/espresso-network/sdks/go/types/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
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

		messages, err := fetchNextHotshotBlock(ctx, mockEspressoClient, blockNum, parseFn, namespace)
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

type mockEspressoTEEVerifier struct {
	mock.Mock
}

func (v *mockEspressoTEEVerifier) Verify(opts *bind.CallOpts, attestation []byte, signature [32]byte) (legacy_espressogen.EnclaveReport, error) {
	return legacy_espressogen.EnclaveReport{}, nil
}

type mockEspressoClient struct {
	mock.Mock
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

func (m *mockEspressoClient) StreamTransactions(ctx context.Context, height uint64) (client.Stream[types.TransactionQueryData], error) {
	panic("not implemented")
}

func (m *mockEspressoClient) StreamTransactionsInNamespace(ctx context.Context, height uint64, namespace uint64) (client.Stream[types.TransactionQueryData], error) {
	panic("not implemented")
}
