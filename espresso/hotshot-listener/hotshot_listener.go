package hotshot_listener

import (
	"context"
	"fmt"
	"math/big"
	"strconv"

	"github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/gorilla/websocket"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"

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
	quorumViewNumberBuilderCommitment map[string]big.Int
	daViewNumberBuilderCommitment     map[string]bool
	sequencerAddress                  string
	conn                              *websocket.Conn
}

func NewHotshotListener(hotshotUrl string, rollupSequencerManagerContract string, l1Client *ethclient.Client, sequencerAddress string) (*HotshotListener, error) {

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

	// Create a new rollup sequencer manager contract instance
	return &HotshotListener{
		hotshotUrl:                        hotshotUrl + HotshotListenerEndpoint,
		rollupSequencerManager:            rollupSequencerManager,
		quorumViewNumberBuilderCommitment: make(map[string]big.Int),
		daViewNumberBuilderCommitment:     make(map[string]bool),
		sequencerAddress:                  sequencerAddress,
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
	listener.quorumViewNumberBuilderCommitment[key] = *l1FinalizedBlockNumberBigInt

	// Check if a da commitment exists for the key relative to
	// this quorum proposal view number and builder commitment
	if _, ok := listener.daViewNumberBuilderCommitment[key]; !ok {
		log.Info("Waiting for Da proposal for the given builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)
		return nil
	}
	log.Info("processing builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)

	// Get the sequencer address for the next view
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

	if sequencerAddressForNextView.Hex() == listener.sequencerAddress {
		log.Info("next view is this node's view", "nextView", nextView, "sequencerAddress", listener.sequencerAddress)
		// TODO: Processing will be implemented in the next PR
	}

	// TODO: Processing will be implemented in the next PR

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
	listener.daViewNumberBuilderCommitment[key] = true
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
	nextView := viewNumber + 1
	// Note: Its important to use l1 finalized block number here because we want the GetCurrentSequencer to
	// always return the same sequencer address for the same view number
	sequencerAddressForNextView, err := listener.rollupSequencerManager.GetCurrentSequencer(&bind.CallOpts{
		BlockNumber: &l1FinalizedBlockNumberForView,
	}, big.NewInt(int64(nextView)))
	if err != nil {
		log.Error("failed to get sequencer address for next view", "err", err)
		return err
	}

	// Check if the sequencer address is the same address of this node
	if sequencerAddressForNextView.Hex() != listener.sequencerAddress {
		log.Info("next view is not this node's view")
		// TODO: Processing will be implemented in the next PR
	}

	// TODO: Processing will be implemented in the next PR

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
