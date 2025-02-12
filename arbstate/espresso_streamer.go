package arbstate

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"

	espressoClient "github.com/EspressoSystems/espresso-sequencer-go/client"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/solgen/go/precompilesgen"
	"github.com/offchainlabs/nitro/util/headerreader"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

const (
	delayedMessageCountMethodName = "delayedMessageCount"
	verifyQuote                   = "verify"
)

type MessageWithMetadataAndPos struct {
	MessageWithMeta arbostypes.MessageWithMetadata
	Pos             uint64
}

type EspressoStreamer struct {
	stopwaiter.StopWaiter
	l1Reader                       *headerreader.HeaderReader
	espressoClients                []espressoClient.Client
	nextHotshotBlockNum            uint64
	namespace                      uint64
	retryTime                      time.Duration
	pollingHotshotPollingInterval  time.Duration
	messageWithMetadataAndPos      []*MessageWithMetadataAndPos
	messagesWithMetadatAndPosMutex sync.Mutex
	bridgeABI                      *abi.ABI
	bridgeAddr                     common.Address
	espressoTEEVerifierABI         *abi.ABI
	espressoTEEVerifierAddr        common.Address
}

func NewEspressoStreamer(namespace uint64, hotshotUrls []string,
	nextHotshotBlockNum uint64,
	retryTime time.Duration,
	pollingHotshotPollingInterval time.Duration,
	bridgeAddr common.Address,
	parentChainNodeUrl string,
	headerReaderConfig headerreader.Config,
) *EspressoStreamer {
	var espressoClients []espressoClient.Client
	for _, url := range hotshotUrls {
		client := espressoClient.NewClient(url)
		if client == nil {
			// we should be able to initialize
			// all the clients
			log.Crit("Failed to create espresso client", "url", url)
			return nil
		}
		espressoClients = append(espressoClients, *client)
	}
	bridgeAbi, err := bridgegen.IBridgeMetaData.GetAbi()
	if err != nil {
		log.Crit("Unable to find Bridge ABI")
	}
	l1Client, err := ethclient.Dial(parentChainNodeUrl)
	if err != nil {
		log.Crit("Failed to create l1 client", "url", parentChainNodeUrl)
		return nil
	}

	arbSys, err := precompilesgen.NewArbSys(types.ArbSysAddress, l1Client)
	if err != nil {
		log.Crit("Failed to create arbsys", "err", err)
		return nil
	}

	// we initialze a l1 reader that will poll for header every 60 seconds
	l1Reader, err := headerreader.New(context.Background(), l1Client, func() *headerreader.Config {
		return &headerReaderConfig
	}, arbSys)

	if err != nil {
		log.Crit("Failed to create l1 reader", "err", err)
		return nil
	}
	return &EspressoStreamer{
		espressoClients:               espressoClients,
		nextHotshotBlockNum:           nextHotshotBlockNum,
		retryTime:                     retryTime,
		pollingHotshotPollingInterval: pollingHotshotPollingInterval,
		namespace:                     namespace,
		bridgeABI:                     bridgeAbi,
		l1Reader:                      l1Reader,
		bridgeAddr:                    bridgeAddr,
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

/* Check if the delayed message is finalized and only then add it to the queue */
func (s *EspressoStreamer) checkDelayedMessageIsFinalized(ctx context.Context, msg *arbostypes.MessageWithMetadata) (bool, error) {
	// Get the latest finalized block header
	header, err := s.l1Reader.LatestFinalizedBlockHeader(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get latest finalized block header: %w", err)
	}
	finalized := header.Number

	// Get the delayedMessageCount method from the ABI
	method, ok := s.bridgeABI.Methods[delayedMessageCountMethodName]
	if !ok {
		return false, fmt.Errorf("failed to find method %s in ABI", delayedMessageCountMethodName)
	}

	// Pack the method call (no arguments needed if delayedMessageCount doesn't take any)
	calldata := method.ID

	// Call the contract at the finalized block
	result, err := s.l1Reader.Client().CallContract(ctx, ethereum.CallMsg{
		To:   &s.bridgeAddr,
		Data: calldata,
	}, finalized)
	if err != nil {
		return false, fmt.Errorf("failed to call contract: %w", err)
	}

	// Unpack the result into a *big.Int
	var delayedMessageCount *big.Int
	delayedMessageCount = new(big.Int)

	// Use method.Outputs.Unpack to decode the result
	decodedResults, err := method.Outputs.Unpack(result)
	if err != nil {
		return false, fmt.Errorf("failed to unpack result: %w", err)
	}

	// Ensure the decoded result is of the expected type and length
	if len(decodedResults) != 1 {
		return false, fmt.Errorf("unexpected number of results: expected 1, got %d", len(decodedResults))
	}

	// Assert the type of the decoded result
	delayedMessageCount, ok = decodedResults[0].(*big.Int)
	if !ok {
		return false, fmt.Errorf("failed to assert result as *big.Int")
	}

	// Log the delayed message count
	log.Info("Delayed message count", "delayedMessageCount", delayedMessageCount)

	// If the delayed message count is less than the finalized delayed message read count,
	// the delayed message count is not finalized yet
	if msg.DelayedMessagesRead < delayedMessageCount.Uint64() {
		return false, fmt.Errorf("delayed message count is less than the finalized delayed message read count")
	}

	return true, nil
}

/* Verify the attestation quote */
func (s *EspressoStreamer) verifyAttestationQuote(ctx context.Context, attestation []byte, userDataHash []byte) error {

	method, ok := s.espressoTEEVerifierABI.Methods[verifyQuote]
	if !ok {
		return fmt.Errorf("verify method not found")
	}

	calldata, err := method.Inputs.Pack(
		attestation,
		userDataHash,
	)

	if err != nil {
		return fmt.Errorf("failed to pack calldata: %w", err)
	}
	fullCalldata := append([]byte{}, method.ID...)
	fullCalldata = append(fullCalldata, calldata...)

	_, err = s.l1Reader.Client().CallContract(
		ctx,
		ethereum.CallMsg{
			To:   &s.espressoTEEVerifierAddr,
			Data: fullCalldata,
		}, nil,
	)
	if err != nil {
		return fmt.Errorf("call to the contract failed: %w", err)
	}

	return nil
}

/**
* Create a queue of messages from the hotshot to be processed by the node
* It will sort the messages by the message index
* and store the messages in `messagesWithMetadata` queue
 */
func (s *EspressoStreamer) queueMessagesFromHotshot(ctx context.Context) error {
	if s.nextHotshotBlockNum == 0 {
		// We dont need to check majority here  because when we eventually go
		// to fetch a block at a certain height,
		// we will check that a quorum of nodes agree on the block at that height,
		// which wouldn't be possible if we were somehow given a height
		// that wasn't finalized at all
		latestBlock, err := s.espressoClients[0].FetchLatestBlockHeight(ctx)
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
		attestation, userDataHash, indices, messages, err := arbutil.ParseHotShotPayload(tx)
		if err != nil {
			log.Warn("failed to parse hotshot payload, will retry", "err", err)
			return err
		}
		// if attestation verification fails, we should skip this message
		// Parse the messages
		err = s.verifyAttestationQuote(ctx, attestation, userDataHash)
		if err != nil {
			log.Warn("failed to verify attestation quote", "err", err)
			continue
		}
		for i, message := range messages {
			var messageWithMetadata arbostypes.MessageWithMetadata
			err = rlp.DecodeBytes(message, &messageWithMetadata)
			if err != nil {
				log.Warn("failed to decode message, will retry", "err", err)
				// Instead of returnning an error, we should just skip this message
				continue
			}

			// Check if the delayed message count is finalized
			shouldIncludeMessage, err := s.checkDelayedMessageIsFinalized(ctx, &messageWithMetadata)
			if err != nil {
				log.Warn("Error while checking if delayed message count is finalized : %w", err)
				// We dont want to skip this message, so we will re-try once the delayed message count is finalized
				return err
			}
			if !shouldIncludeMessage {
				log.Warn("Delayed message count is not finalized yet")
				return fmt.Errorf("delayed message count is not finalized yet")
			}

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
