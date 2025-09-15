package decentralized_timeboost_types

import (
	"github.com/ethereum/go-ethereum/common"
)

type BatchPosterArgs struct {
	SequencerNumber          uint64         `json:"sequencerNumber"`
	AfterDelayedMessagesRead uint64         `json:"afterDelayedMessagesRead"`
	GasRefunder              common.Address `json:"gasRefunder"`
	PreviousMessageCount     uint64         `json:"previousMessageCount"`
	NewMessageCount          uint64         `json:"newMessageCount"`
	Data                     []byte         `json:"data"`
}
