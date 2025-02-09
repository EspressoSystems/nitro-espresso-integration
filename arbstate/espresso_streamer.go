package arbstate

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	espressoClient "github.com/EspressoSystems/espresso-sequencer-go/client"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type MessageWithMetadataAndPos struct {
	MessageWithMeta arbostypes.MessageWithMetadata
	Pos             uint64
}

type EspressoStreamer struct {
	stopwaiter.StopWaiter

	espressoClients                []espressoClient.Client
	nextHotshotBlockNum            uint64
	namespace                      uint64
	retryTime                      time.Duration
	pollingHotshotPollingInterval  time.Duration
	messageWithMetadataAndPos      []*MessageWithMetadataAndPos
	messagesWithMetadatAndPosMutex sync.Mutex
}

func NewEspressoStreamer(namespace uint64, hotshotUrls []string, nextHotshotBlockNum uint64, retryTime time.Duration, pollingHotshotPollingInterval time.Duration) *EspressoStreamer {
	var espressoClients []espressoClient.Client
	for _, url := range hotshotUrls {
		client := espressoClient.NewClient(url)
		if client == nil {
			log.Crit("Failed to create espresso client", "url", url)
			// we should be able to initialize
			// all the clients
			return nil
		}
		espressoClients = append(espressoClients, *client)
	}
	return &EspressoStreamer{
		espressoClients:               espressoClients,
		nextHotshotBlockNum:           nextHotshotBlockNum,
		retryTime:                     retryTime,
		pollingHotshotPollingInterval: pollingHotshotPollingInterval,
		namespace:                     namespace,
	}
}

/*
Pop the first message from the queue
It will return nil if the queue is empty
*/
func (s *EspressoStreamer) PopMessageWithMetadataAndPos() *MessageWithMetadataAndPos {
	s.messagesWithMetadatAndPosMutex.Lock()
	defer s.messagesWithMetadatAndPosMutex.Unlock()
	if len(s.messageWithMetadataAndPos) == 0 {
		return nil
	}
	messageWithMetadataAndPos := s.messageWithMetadataAndPos[0]
	s.messageWithMetadataAndPos = s.messageWithMetadataAndPos[1:]
	return messageWithMetadataAndPos
}

/*
Peek the first message from the queue
It will return nil if the queue is empty
This will be used to check if the the EspressoStreamer has recieved
the next expected message in sequence
*/
func (s *EspressoStreamer) PeekMessageWithMetadataAndPos() *MessageWithMetadataAndPos {
	s.messagesWithMetadatAndPosMutex.Lock()
	defer s.messagesWithMetadatAndPosMutex.Unlock()
	if len(s.messageWithMetadataAndPos) == 0 {
		return nil
	}
	return s.messageWithMetadataAndPos[0]
}

/**
* Create a queue of messages from the hotshot to be processed by the node
* It will sort the messages by the message index
* and store the messages in `messagesWithMetadata` queue
 */
func (s *EspressoStreamer) queueMessagesFromHotshot(ctx context.Context) error {
	if s.nextHotshotBlockNum == 0 {
		latestBlock, err := s.fetchLatestBlockFromAllClients(ctx)
		if err != nil {
			log.Warn("unable to fetch latest hotshot block", "err", err)
			return err
		}
		log.Info("Started node at the latest hotshot block", "block number", latestBlock)
		s.nextHotshotBlockNum = latestBlock
	}

	nextHotshotBlockNum := s.nextHotshotBlockNum

	// TODO: should support multiple clients
	arbTxns, err := s.espressoClients[0].FetchTransactionsInBlock(ctx, nextHotshotBlockNum, s.namespace)
	if err != nil {
		log.Warn("failed to fetch the transactions", "err", err)
		return err
	}
	if len(arbTxns.Transactions) == 0 {
		log.Info("No transactions found in the hotshot block", "block number", nextHotshotBlockNum)
		return nil
	}

	s.messagesWithMetadatAndPosMutex.Lock()
	defer s.messagesWithMetadatAndPosMutex.Unlock()

	for _, tx := range arbTxns.Transactions {
		// Parse hotshot payload
		_, indices, messages, err := arbutil.ParseHotShotPayload(tx)
		if err != nil {
			log.Warn("failed to parse hotshot payload, will retry", "err", err)
			return err
		}
		// Parse the messages
		for i, message := range messages {
			var messageWithMetadata arbostypes.MessageWithMetadata
			err = rlp.DecodeBytes(message, &messageWithMetadata)
			if err != nil {
				log.Warn("failed to decode message, will retry", "err", err)
				// Instead of returnning an error, we should just skip this message
				continue
			}
			// TODO: add attestation verification here
			// if attestation verification fails, we should skip this message

			// Only append to the slice if the message is not a duplicate
			if !s.messageWithMetadataAndPosContains(indices[i]) {
				s.messageWithMetadataAndPos = append(s.messageWithMetadataAndPos, &MessageWithMetadataAndPos{
					MessageWithMeta: messageWithMetadata,
					Pos:             indices[i],
				})
				log.Info("Added message to queue", "message", indices[i])
			}
		}
	}

	// Sort the messagesWithMetadataAndPos based on ascending order
	// This is to ensure that we process messages in the correct order
	sort.SliceStable(s.messageWithMetadataAndPos, func(i, j int) bool {
		return s.messageWithMetadataAndPos[i].Pos < s.messageWithMetadataAndPos[j].Pos
	})

	return nil
}

/*
Fetch the latest block from all the clients
If any of the clients has a majority of the latest block, return that block
Otherwise return 0
*/
func (s *EspressoStreamer) fetchLatestBlockFromAllClients(ctx context.Context) (uint64, error) {
	var latestBlockMap = make(map[uint64]int)
	for _, client := range s.espressoClients {
		latestBlock, err := client.FetchLatestBlockHeight(ctx)
		if err != nil {
			log.Warn("unable to fetch latest hotshot block", "err", err)
			return 0, err
		}
		// If the latest block is already in the map, increase the count
		// Otherwise add the latest block to the map
		if latestBlockMap[latestBlock] > 0 {
			latestBlockMap[latestBlock] += 1
		} else {
			latestBlockMap[latestBlock] = 1
		}
	}

	// Check if any latestBlock has a count greater than 50%
	for latestBlock, count := range latestBlockMap {
		if count > len(s.espressoClients)/2 {
			return latestBlock, nil
		}
	}

	return 0, fmt.Errorf("unable to find a latest block which has a majority of the clients")
}

/*
Check if the message with metadata and pos is already in the queue
*/
func (s *EspressoStreamer) messageWithMetadataAndPosContains(pos uint64) bool {
	for _, messageWithMetadataAndPos := range s.messageWithMetadataAndPos {
		if messageWithMetadataAndPos.Pos == pos {
			return true
		}
	}
	return false
}

func (s *EspressoStreamer) Start(ctxIn context.Context) error {
	s.StopWaiter.Start(ctxIn, s)

	err := s.CallIterativelySafe(func(ctx context.Context) time.Duration {
		err := s.queueMessagesFromHotshot(ctx)
		if err != nil {
			return s.retryTime
		}
		s.nextHotshotBlockNum += 1
		log.Info("Now processing hotshot block", "block number", s.nextHotshotBlockNum)
		return s.pollingHotshotPollingInterval
	})
	return err
}
