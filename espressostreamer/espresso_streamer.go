package espressostreamer

import (
	"context"
	"fmt"
	"sort"
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

type MessageWithMetadataAndPos struct {
	MessageWithMeta arbostypes.MessageWithMetadata
	Pos             uint64
}

type EspressoStreamer struct {
	stopwaiter.StopWaiter
	l1Reader                      *headerreader.HeaderReader
	espressoClients               []espressoClient.Client
	nextHotshotBlockNum           uint64
	namespace                     uint64
	retryTime                     time.Duration
	pollingHotshotPollingInterval time.Duration
	messageWithMetadataAndPos     []*MessageWithMetadataAndPos
	espressoTEEVerifierABI        *abi.ABI
	espressoTEEVerifierAddr       common.Address
}

func NewEspressoStreamer(namespace uint64, hotshotUrls []string,
	nextHotshotBlockNum uint64,
	retryTime time.Duration,
	pollingHotshotPollingInterval time.Duration,
	parentChainNodeUrl string,
	headerReaderConfig headerreader.Config,
	espressoTEEVerifierAddress common.Address,
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

	espressoTEEVerifierAbi, err := bridgegen.IEspressoTEEVerifierMetaData.GetAbi()
	if err != nil {
		log.Crit("Unable to find EspressoTEEVerifier ABI")
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
		l1Reader:                      l1Reader,
		espressoTEEVerifierABI:        espressoTEEVerifierAbi,
		espressoTEEVerifierAddr:       espressoTEEVerifierAddress,
	}
}

/*
Pop the first message from the queue
It will return nil if the queue is empty
*/
func (s *EspressoStreamer) PopMessageWithMetadataAndPos() *MessageWithMetadataAndPos {
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
	if len(s.messageWithMetadataAndPos) == 0 {
		return nil
	}
	return s.messageWithMetadataAndPos[0]
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
		// which wouldn't be possible if we were somehow are given a height
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

	for _, tx := range arbTxns.Transactions {
		// Parse hotshot payload
		attestation, userDataHash, indices, messages, err := arbutil.ParseHotShotPayload(tx)
		if err != nil {
			log.Warn("failed to parse hotshot payload, will retry", "err", err)
			return err
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
				log.Warn("failed to decode message, will retry", "err", err)
				// Instead of returnning an error, we should just skip this message
				continue
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
