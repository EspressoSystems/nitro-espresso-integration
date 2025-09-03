package hotshot_listener

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/gorilla/websocket"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/espresso/utils"
	view_store "github.com/offchainlabs/nitro/espresso/view-store"
	"github.com/offchainlabs/nitro/execution/gethexec"
	"github.com/offchainlabs/nitro/solgen/go/espressogen"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

const (
	HotshotListenerEndpoint = "/hotshot-events/events"
)

type HotshotListener struct {
	stopwaiter.StopWaiter
	hotshotUrl                        string
	rollupSequencerManager            *espressogen.IEspressoRollupSequencerManager
	quorumViewNumberBuilderCommitment map[string]*big.Int
	daViewNumberBuilderCommitment     map[string]*types.BlockPayload
	sequencerAddress                  string
	conn                              *websocket.Conn
	chainId                           uint32
	execution                         *gethexec.ExecutionEngine
	root                              *view_store.ViewStoreBinaryTree
}

func NewHotshotListener(hotshotUrl string, rollupSequencerManagerContract string, l1Client *ethclient.Client, sequencerAddress string, chainId uint32, execution *gethexec.ExecutionEngine) (*HotshotListener, error) {

	if hotshotUrl == "" {
		return nil, fmt.Errorf("hotshot url is empty, please provide a valid url")
	}
	if rollupSequencerManagerContract == "" {
		return nil, fmt.Errorf("rollup sequencer manager contract address is empty, please provide a valid address")
	}
	if sequencerAddress == "" {
		return nil, fmt.Errorf("sequencer address is empty, please provide a valid address")
	}
	if l1Client == nil {
		return nil, fmt.Errorf("l1 client is nil, please provide a valid client")
	}

	// Convert rollupSequencerManagerContract to an address
	rollupSequencerManagerContractAddress := common.HexToAddress(rollupSequencerManagerContract)
	rollupSequencerManager, err := espressogen.NewIEspressoRollupSequencerManager(rollupSequencerManagerContractAddress, l1Client)
	if err != nil {
		log.Error("failed to create rollup sequencer manager contract instance", "err", err)
		return nil, err
	}

	// Create a new view store binary tree

	// Create a new rollup sequencer manager contract instance
	return &HotshotListener{
		hotshotUrl:                        hotshotUrl + HotshotListenerEndpoint,
		rollupSequencerManager:            rollupSequencerManager,
		quorumViewNumberBuilderCommitment: make(map[string]*big.Int),
		daViewNumberBuilderCommitment:     make(map[string]*types.BlockPayload),
		sequencerAddress:                  sequencerAddress,
		chainId:                           chainId,
		execution:                         execution,
		root:                              nil,
	}, nil
}

func (listener *HotshotListener) processMessage(message []byte) error {
	// Convert message to ConsensusMessage
	consensusMessage, err := types.UnmarshalConsensusMessage(message)
	if err != nil {
		log.Error("failed to unmarshal consensus message:", err)
		return err
	}
	// Quorum proposal represents a proposal that needs to be supported by a quorum of nodes
	// this quorum proposal needs to be for a given view and builder commitment
	if consensusMessage.Event.QuorumProposalWrapper != nil {
		return listener.processQuorumProposalEvent(consensusMessage.Event.QuorumProposalWrapper)
	}
	// DA proposal event indicates thats data availability information
	// is available for a given block with the given view number and builder commitment
	if consensusMessage.Event.DaProposalWrapper != nil {
		return listener.processDaProposalEvent(consensusMessage.Event.DaProposalWrapper)
	}

	// Only when hotshot builder has both quorum proposal and DA proposal for a given view
	// it begins constructing another block
	// Decide event in hotshot is the event when
	// a view has been finalized by hotshot and cannot change now
	if consensusMessage.Event.Decide != nil {
		return listener.processDecideEvent(consensusMessage.Event.Decide)
	}

	return nil
}

func (listener *HotshotListener) processQuorumProposalEvent(quorumProposalWrapper *types.QuorumProposalWrapper) error {
	log.Info("received quorum proposal event", "event", quorumProposalWrapper)

	viewNumber := quorumProposalWrapper.QuorumProposalDataWrapper.Data.Proposal.ViewNumber
	builderCommitment := quorumProposalWrapper.QuorumProposalDataWrapper.Data.Proposal.BlockHeader.Fields.BuilderCommitment

	viewNumberString := strconv.Itoa(viewNumber)

	// Combine the hexViewNumber and builderCommitment to get the key
	key := viewNumberString + builderCommitment

	l1FinalizedBlockNumberForView := quorumProposalWrapper.QuorumProposalDataWrapper.Data.Proposal.BlockHeader.Fields.L1Finalized.Number
	l1FinalizedBlockNumberBigInt := big.NewInt(int64(l1FinalizedBlockNumberForView))
	// Store the finalized L1 block number in the map
	listener.quorumViewNumberBuilderCommitment[key] = l1FinalizedBlockNumberBigInt

	// Check if a da commitment exists for the key relative to
	// this quorum proposal view number and builder commitment
	if _, ok := listener.daViewNumberBuilderCommitment[key]; !ok {
		log.Info("Waiting for Da proposal for the given builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)
		return nil
	}
	log.Info("processing builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)

	// Get the sequencer address for the next view
	// #nosec G115
	err := listener.processHotshotCurrentView(listener.daViewNumberBuilderCommitment[key], uint64(viewNumber), builderCommitment)
	if err != nil {
		log.Error("failed to process current view", "err", err)
		return err
	}

	// #nosec G115
	err = listener.processHotshotNextView(l1FinalizedBlockNumberBigInt, uint64(viewNumber))
	if err != nil {
		log.Error("failed to process next view", "err", err)
		return err
	}

	// Delete the quorum and da proposal keys from the map
	// so that map doesnt take a lot of space in memory
	delete(listener.quorumViewNumberBuilderCommitment, key)
	delete(listener.daViewNumberBuilderCommitment, key)
	return nil
}

func (listener *HotshotListener) processDaProposalEvent(daProposalWrapper *types.DaProposalWrapper) error {
	log.Info("received DA Proposal event", "event", daProposalWrapper)

	// Now get the view number for the given builder commitment
	viewNumber := daProposalWrapper.DaProposalDataWrapper.Data.ViewNumber

	viewNumberString := strconv.Itoa(viewNumber)

	blockPayload, err := types.NewBlockPayload(daProposalWrapper.DaProposalDataWrapper.Data.EncodedTransactions,
		daProposalWrapper.DaProposalDataWrapper.Data.Metadata)
	if err != nil {
		return err
	}
	builderCommitment, err := blockPayload.BuilderCommitment()
	if err != nil {
		return err
	}

	builderCommitmentString, err := builderCommitment.ToTaggedSting()
	if err != nil {
		log.Error("failed to convert builder commitment to tagged string:", err)
		return err
	}

	key := viewNumberString + builderCommitmentString

	// Now store the key and check if a quorum proposal exists for the given builder commitment
	listener.daViewNumberBuilderCommitment[key] = blockPayload
	// Check if a da commitment exists for this key
	// relative to this DA proposal view number and builder commitment
	if _, ok := listener.quorumViewNumberBuilderCommitment[key]; !ok {
		// If it does, then we can assume that this is a DA proposal
		log.Info("waiting for Quorum proposal for the given builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitmentString)
		return nil
	}

	// Process the DA proposal and quorum proposal
	log.Info("processing builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitmentString)

	// Get L1 block number from the quorum proposal map
	l1FinalizedBlockNumberForView := listener.quorumViewNumberBuilderCommitment[key]
	if l1FinalizedBlockNumberForView == nil {
		log.Error("l1 finalized block number for view is nil")
		return nil
	}

	// #nosec G115
	err = listener.processHotshotCurrentView(listener.daViewNumberBuilderCommitment[key], uint64(viewNumber), builderCommitmentString)
	if err != nil {
		log.Error("failed to process current view", "err", err)
		return err
	}
	err = listener.processHotshotNextView(l1FinalizedBlockNumberForView, uint64(viewNumber))
	if err != nil {
		log.Error("failed to process next view", "err", err)
		return err
	}

	// Delate the quorum and da proposal keys from the map
	// so that map doesnt take a lot of space in memory
	delete(listener.quorumViewNumberBuilderCommitment, key)
	delete(listener.daViewNumberBuilderCommitment, key)
	return nil

}

func (listener *HotshotListener) processDecideEvent(decide *types.Decide) error {
	log.Info("Received Decide event", "event", decide)
	for _, leafChain := range decide.LeafChain {
		// Check if any of the leafs match the view number + builder commitment that we have stored
		viewNumber := leafChain.Leaf.ViewNumber
		builderCommitment := leafChain.Leaf.BlockHeader.Fields.BuilderCommitment
		log.Info("processing leaf chain", "leafChain", leafChain, "builderCommitment", builderCommitment, "viewNumber", viewNumber)
		// TODO: Processing will be implemented in the next PR
	}
	return nil
}

func (listener *HotshotListener) processHotshotNextView(l1FinalizedBlockNumberBigInt *big.Int, viewNumber uint64) error {
	nextView := viewNumber + 1

	// Note: Its important to use l1 finalized block number here because we want the GetCurrentSequencer to
	// always return the same sequencer address for the same view number
	sequencerAddressForNextView, err := listener.rollupSequencerManager.GetCurrentSequencer(&bind.CallOpts{
		BlockNumber: l1FinalizedBlockNumberBigInt,
	}, big.NewInt(int64(nextView)))
	if err != nil {
		log.Error("failed to get current sequencer", "err", err)
		return err
	}

	if sequencerAddressForNextView.Hex() != listener.sequencerAddress {
		// TODO: Processing will be implemented in the next PR
		return nil
	}
	log.Info("next view is this node's view", "nextView", nextView, "sequencerAddress", listener.sequencerAddress)
	// TODO: Processing will be implemented in the next PR
	return nil
}

func (listener *HotshotListener) processHotshotCurrentView(blockPayload *types.BlockPayload, viewNumber uint64, builderCommitment string) error {
	log.Info("received hotshot view event")
	// Decode the block payload
	encodedTransactions := blockPayload.RawPayload

	nsRange, err := utils.DecodeNSTable(blockPayload.NsTable.Bytes, listener.chainId)
	if err != nil {
		return err
	}

	if nsRange == nil {
		return err
	}

	transactionsPayload, err := utils.DecodeTransactionsPayload(encodedTransactions, *nsRange)
	if err != nil {
		return err
	}

	for _, tx := range transactionsPayload {
		_, _, indices, messages, err := arbutil.ParseHotShotPayload(tx.Payload)
		if err != nil {
			return err
		}
		if len(messages) == 0 {
			return nil
		}

		err = listener.processMessages(messages, indices)
		if err != nil {
			log.Error("failed to process messages", "err", err)
			return err
		}
	}

	// Now add the state hash to the view store
	lastBlockHeader := listener.execution.Bc().CurrentBlock().Hash()

	// Update the view store
	viewStoreBinaryTree := view_store.Insert(listener.root, viewNumber, builderCommitment, lastBlockHeader)
	listener.root = viewStoreBinaryTree
	log.Info("view store updated", "viewStore", viewStoreBinaryTree)
	return nil
}

func (listener *HotshotListener) processMessages(messages [][]byte, indices []uint64) error {

	// Now we need to convert the transaction payload to a nitro block and submit it to the sequencer
	currentBlockNumber := listener.execution.Bc().CurrentBlock().Number.Uint64()

	for i, message := range messages {
		var messageWithMetadata arbostypes.MessageWithMetadata
		err := rlp.DecodeBytes(message, &messageWithMetadata)
		if err != nil {
			log.Warn("failed to decode message", "err", err)
			// Instead of returnning an error, we should just skip this message
			continue
		}

		if indices[i] <= currentBlockNumber {
			log.Warn("message index is less than current message pos, skipping", "messageIndex", indices[i], "currentMessagePos", currentBlockNumber)
			continue
		}
		err = listener.createBlock(&messageWithMetadata)
		if err != nil {
			log.Error("unable to create block", "err", err)
			return err
		}
	}

	// At the end store the state hash in th view store

	return nil
}

func (listener *HotshotListener) createBlock(msg *arbostypes.MessageWithMetadata) error {

	lastBlockHeader := listener.execution.Bc().CurrentBlock()

	statedb, err := listener.execution.Bc().StateAt(lastBlockHeader.Root)
	if err != nil {
		log.Error("failed to get state at last block header", "err", err)
		return err
	}

	startTime := time.Now()
	block, receipts, err := arbos.ProduceBlock(msg.Message, msg.DelayedMessagesRead, lastBlockHeader, statedb, listener.execution.Bc(), false, core.MessageReplayMode)

	if err != nil || block == nil {
		log.Error("Failed to produce block", "err", err)
		return err
	}

	blockCalcTime := time.Since(startTime)

	log.Info("Produced block", "block", block.Hash(), "blockNumber", block.Number(), "receipts", len(receipts))

	err = listener.execution.AppendBlock(block, statedb, receipts, blockCalcTime)
	if err != nil {
		log.Error("Failed to append block", "err", err)
		return err
	}

	return nil
}

func (listener *HotshotListener) Start(ctx context.Context) error {
	listener.StopWaiter.Start(ctx, listener)
	conn, _, err := websocket.DefaultDialer.Dial(listener.hotshotUrl, nil)
	if err != nil {
		log.Error("failed to connect to hotshot webSocket", "err", err)
		return err
	}
	listener.conn = conn

	// Launch thread to listen to new messages from the websocket
	listener.LaunchThread(func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				listener.conn.Close()
				return
			default:
			}
			_, message, err := listener.conn.ReadMessage()
			if err != nil {
				log.Error("error reading message", "err", err)
				continue
			}
			err = listener.processMessage(message)
			if err != nil {
				log.Error("error processing message", "err", err)
				continue
			}
		}
	})

	return nil
}

func (listener *HotshotListener) StopAndWait() {
	err := listener.conn.Close()
	if err != nil {
		log.Error("failed to close websocket connection", "err", err)
	}
	listener.StopWaiter.StopAndWait()
}
