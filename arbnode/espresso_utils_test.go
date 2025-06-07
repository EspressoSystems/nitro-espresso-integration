package arbnode

import (
	"bytes"
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/util/signature"
)

func TestRecoverAddressFromSigner(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	address, err := recoverAddressFromSigner(signature.DataSignerFromPrivateKey(privateKey))
	if err != nil {
		t.Fatal(err)
	}
	if address != crypto.PubkeyToAddress(privateKey.PublicKey) {
		t.Fatalf("expected address %v, got %v", crypto.PubkeyToAddress(privateKey.PublicKey), address)
	}
}

func TestGetMessageForSubmittingToEspresso(t *testing.T) {

	messages := []*arbostypes.MessageWithMetadata{
		{
			Message: &arbostypes.L1IncomingMessage{
				Header: &arbostypes.L1IncomingMessageHeader{},
				L2msg:  []byte{1}},
			DelayedMessagesRead: 1,
		},
		{
			Message: &arbostypes.L1IncomingMessage{
				Header: &arbostypes.L1IncomingMessageHeader{},
				L2msg:  []byte{2}},
			DelayedMessagesRead: 1,
		},
		{
			Message: &arbostypes.L1IncomingMessage{
				Header: &arbostypes.L1IncomingMessageHeader{},
				L2msg:  []byte{3}},
			DelayedMessagesRead: 2,
		},
		{
			Message:             &arbostypes.L1IncomingMessage{Header: &arbostypes.L1IncomingMessageHeader{}, L2msg: []byte{4}},
			DelayedMessagesRead: 2,
		},
	}

	fetcher := func(pos arbutil.MessageIndex) (*arbostypes.MessageWithMetadata, error) {
		return messages[pos], nil
	}

	msg, err := getMessageForSubmittingToEspresso(0, fetcher)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(msg.Message.L2msg, []byte{1}) {
		t.Fatalf("expected message with L2msg [1], got %v", msg.Message.L2msg)
	}

	msg, err = getMessageForSubmittingToEspresso(1, fetcher)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(msg.Message.L2msg, []byte{2}) {
		t.Fatalf("expected message with L2msg [2], got %v", msg.Message.L2msg)
	}

	msg, err = getMessageForSubmittingToEspresso(2, fetcher)
	if err != nil {
		t.Fatal(err)
	}
	// Should be empty because this is delayed message
	if !bytes.Equal(msg.Message.L2msg, []byte{}) {
		t.Fatalf("expected message with L2msg [3], got %v", msg.Message.L2msg)
	}

	msg, err = getMessageForSubmittingToEspresso(3, fetcher)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(msg.Message.L2msg, []byte{4}) {
		t.Fatalf("expected message with L2msg [4], got %v", msg.Message.L2msg)
	}
}

func TestBinarySearchForBlockNumber(t *testing.T) {
	target := uint64(64)
	count := 0
	ctx := context.Background()
	start := uint64(0)
	end := uint64(100)
	f := func(ctx context.Context, blockNumber uint64) (int, error) {
		count++
		if blockNumber < target {
			return -1, nil
		} else if blockNumber > target {
			return 1, nil
		}
		return 0, nil
	}
	result, err := binarySearchForBlockNumber(ctx, start, end, f)
	if err != nil {
		t.Fatal(err)
	}
	if result != target {
		t.Fatalf("expected result %d, got %d", target, result)
	}
	if count > 7 {
		t.Fatalf("expected count less than %d, got %d", 7, count)
	}

	targetRangeStart := uint64(60)
	targetRangeEnd := uint64(70)
	count = 0
	f = func(ctx context.Context, blockNumber uint64) (int, error) {
		count++
		if blockNumber < targetRangeStart {
			return -1, nil
		} else if blockNumber > targetRangeEnd {
			return 1, nil
		}
		return 0, nil
	}
	result, err = binarySearchForBlockNumber(ctx, start, end, f)
	if err != nil {
		t.Fatal(err)
	}
	if result != targetRangeStart {
		t.Fatalf("expected result %d, got %d", targetRangeStart, result)
	}
	if count > 7 {
		t.Fatalf("expected count less than %d, got %d", 7, count)
	}
}
