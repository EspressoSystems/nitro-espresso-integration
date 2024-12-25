// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package arbnode

var (
	messagePrefix                []byte = []byte("m") // maps a message sequence number to a message
	blockHashInputFeedPrefix     []byte = []byte("b") // maps a message sequence number to a block hash received through the input feed
	messageResultPrefix          []byte = []byte("r") // maps a message sequence number to a message result
	legacyDelayedMessagePrefix   []byte = []byte("d") // maps a delayed sequence number to an accumulator and a message as serialized on L1
	rlpDelayedMessagePrefix      []byte = []byte("e") // maps a delayed sequence number to an accumulator and an RLP encoded message
	parentChainBlockNumberPrefix []byte = []byte("p") // maps a delayed sequence number to a parent chain block number
	sequencerBatchMetaPrefix     []byte = []byte("s") // maps a batch sequence number to BatchMetadata
	delayedSequencedPrefix       []byte = []byte("a") // maps a delayed message count to the first sequencer batch sequence number with this delayed count

	messageCountKey              []byte = []byte("_messageCount")                 // contains the current message count
	delayedMessageCountKey       []byte = []byte("_delayedMessageCount")          // contains the current delayed message count
	sequencerBatchCountKey       []byte = []byte("_sequencerBatchCount")          // contains the current sequencer message count
	dbSchemaVersion              []byte = []byte("_schemaVersion")                // contains a uint64 representing the database schema version
	espressoSubmittedPos         []byte = []byte("_espressoSubmittedPos")         // contains the current message indices of the last submitted txns
	espressoSubmittedHash        []byte = []byte("_espressoSubmittedHash")        // contains the hash of the last submitted txn
	espressoSubmittedPayload     []byte = []byte("_espressoSubmittedPayload")     // contains the payload of the last submitted espresso txn
	espressoPendingTxnsPositions []byte = []byte("_espressoPendingTxnsPositions") // contains the index of the pending txns that need to be submitted to espresso
	espressoLastConfirmedPos     []byte = []byte("_espressoLastConfirmedPos")     // contains the position of the last confirmed message
)

const currentDbSchemaVersion uint64 = 1
