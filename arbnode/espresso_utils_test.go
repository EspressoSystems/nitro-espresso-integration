package arbnode

import (
	"bytes"
	"testing"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
)

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
