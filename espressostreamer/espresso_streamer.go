package espressostreamer

import (
	"context"
	"fmt"
	"sync"
	"time"
  "math/big"

	espressoClient "github.com/EspressoSystems/espresso-sequencer-go/client"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)
type ParentChainClientInterface interface{
  CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

type EspressoClientInterface interface{
  FetchLatestBlockHeight(ctx context.Context) (uint64, error)
  FetchTransactionsInBlock(ctx context.Context, blockHeight uint64, namespace uint64) (espressoClient.TransactionsInBlock, error)
}

type MessageWithMetadataAndPos struct {
	MessageWithMeta arbostypes.MessageWithMetadata
	Pos             uint64
	HotshotHeight   uint64
}

type EspressoStreamer struct {
	stopwaiter.StopWaiter
	l1Reader                      ParentChainClientInterface
	espressoClient                EspressoClientInterface
	nextHotshotBlockNum           uint64
	currentMessagePos             uint64
	namespace                     uint64
	retryTime                     time.Duration
	pollingHotshotPollingInterval time.Duration
	messageWithMetadataAndPos     []*MessageWithMetadataAndPos
	espressoTEEVerifierABI        *abi.ABI
	espressoTEEVerifierAddr       common.Address

	messageMutex sync.Mutex
}

func NewEspressoStreamer(namespace uint64, hotshotUrls []string,
	nextHotshotBlockNum uint64,
	retryTime time.Duration,
	pollingHotshotPollingInterval time.Duration,
	espressoTEEVerifierAddress common.Address,
  l1Reader ParentChainClientInterface,
  espressoClient EspressoClientInterface,
) *EspressoStreamer {

	espressoTEEVerifierAbi, err := bridgegen.IEspressoTEEVerifierMetaData.GetAbi()
	if err != nil {
		log.Crit("Unable to find EspressoTEEVerifier ABI")
	}

		if err != nil {
		log.Crit("Failed to create l1 reader", "err", err)
		return nil
	}
	return &EspressoStreamer{
		espressoClient:                espressoClient,
		nextHotshotBlockNum:           nextHotshotBlockNum,
		retryTime:                     retryTime,
		pollingHotshotPollingInterval: pollingHotshotPollingInterval,
		namespace:                     namespace,
		l1Reader:                      l1Reader,
		espressoTEEVerifierABI:        espressoTEEVerifierAbi,
		espressoTEEVerifierAddr:       espressoTEEVerifierAddress,
	}
}

func (s *EspressoStreamer) Reset(currentMessagePos uint64, currentHostshotBlock uint64) {
	s.messageMutex.Lock()
	defer s.messageMutex.Unlock()
	s.currentMessagePos = currentMessagePos
	s.nextHotshotBlockNum = currentHostshotBlock
	s.messageWithMetadataAndPos = []*MessageWithMetadataAndPos{}
}
// Next filters over the espresso streamers buffer and, if it is non-empty,
// returns a reference to a MessageWithMetadataAndPos. Calling Next() when the buffer is empty,
// or there is no element in the list with position s.currentMessagePos, will result in an error
// 
// If err is nil it can be assumed that the message returned is non nil.
func (s *EspressoStreamer) Next() (*MessageWithMetadataAndPos, error) {
	s.messageMutex.Lock()
	defer s.messageMutex.Unlock()

	message, found := FilterAndFind(&s.messageWithMetadataAndPos, func(msg *MessageWithMetadataAndPos) int {
		if msg.Pos == s.currentMessagePos {
			return 0
		}
		if msg.Pos < s.currentMessagePos {
			return -1
		}
		return 1
	})
	if !found {
		return nil, fmt.Errorf("no message found")
	}
	s.currentMessagePos += 1
	return message, nil
}

/* Verify the attestation quote */
func (s *EspressoStreamer) verifyAttestationQuote(ctx context.Context, attestation []byte, userDataHash [32]byte) error {

	method, ok := s.espressoTEEVerifierABI.Methods["verify"]
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

	_, err = s.l1Reader.CallContract(
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
func (s *EspressoStreamer) QueueMessagesFromHotshot(ctx context.Context) error {
	// Note: Adding the lock on top level
	// because s.nextHotshotBlockNum is updated if n.nextHotshotBlockNum == 0
	s.messageMutex.Lock()
	defer s.messageMutex.Unlock()

	if s.nextHotshotBlockNum == 0 {
		// We dont need to check majority here  because when we eventually go
		// to fetch a block at a certain height,
		// we will check that a quorum of nodes agree on the block at that height,
		// which wouldn't be possible if we were somehow are given a height
		// that wasn't finalized at all
		latestBlock, err := s.espressoClient.FetchLatestBlockHeight(ctx)
		if err != nil {
			log.Warn("unable to fetch latest hotshot block", "err", err)
			return err
		}
		log.Info("Started node at the latest hotshot block", "block number", latestBlock)
		s.nextHotshotBlockNum = latestBlock
	}

	arbTxns, err := s.espressoClient.FetchTransactionsInBlock(ctx, s.nextHotshotBlockNum, s.namespace)
	if err != nil {
		log.Warn("failed to fetch the transactions", "err", err)
		return err
	}

	if len(arbTxns.Transactions) == 0 {
		log.Info("No transactions found in the hotshot block", "block number", s.nextHotshotBlockNum)
		s.nextHotshotBlockNum += 1
		return nil
	}

	for _, tx := range arbTxns.Transactions {
		// Parse hotshot payload
		attestation, userDataHash, indices, messages, err := arbutil.ParseHotShotPayload(tx)
		if err != nil {
			log.Warn("failed to parse hotshot payload", "err", err)
			continue
		}
		// if attestation verification fails, we should skip this message
		// Parse the messages
		if len(userDataHash) != 32 {
			log.Warn("user data hash is not 32 bytes")
			continue
		}
		userDataHashArr := [32]byte(userDataHash)
		err = s.verifyAttestationQuote(ctx, attestation, userDataHashArr)
		if err != nil {
			log.Warn("failed to verify attestation quote", "err", err)
			continue
		}
		for i, message := range messages {
			var messageWithMetadata arbostypes.MessageWithMetadata
			err = rlp.DecodeBytes(message, &messageWithMetadata)
			if err != nil {
				log.Warn("failed to decode message", "err", err)
				// Instead of returnning an error, we should just skip this message
				continue
			}
			if indices[i] < s.currentMessagePos {
				log.Warn("message index is less than current message pos, skipping", "messageIndex", indices[i], "currentMessagePos", s.currentMessagePos)
				continue
			}
			s.messageWithMetadataAndPos = append(s.messageWithMetadataAndPos, &MessageWithMetadataAndPos{
				MessageWithMeta: messageWithMetadata,
				Pos:             indices[i],
				HotshotHeight:   s.nextHotshotBlockNum,
			})
			log.Info("Added message to queue", "message", indices[i])
		}
	}

	s.nextHotshotBlockNum += 1

	return nil
}

func (s *EspressoStreamer) Start(ctxIn context.Context) error {
	s.StopWaiter.Start(ctxIn, s)
	err := s.CallIterativelySafe(func(ctx context.Context) time.Duration {
		err := s.QueueMessagesFromHotshot(ctx)
		if err != nil {
			log.Error("error while queueing messages from hotshot", "err", err)
			return s.retryTime
		}
		log.Info("Now processing hotshot block", "block number", s.nextHotshotBlockNum)
		return s.pollingHotshotPollingInterval
	})
	return err
}
