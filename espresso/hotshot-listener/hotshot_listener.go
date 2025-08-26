package hotshot_listener

import (
	"context"
	"fmt"
	"math/big"

	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/solgen/go/espressogen"
	"github.com/offchainlabs/nitro/util/stopwaiter"

	"github.com/gorilla/websocket"
)

const (
	HotshotListenerEndpoint = "/v1/hotshot-events/events"
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
		log.Error("Failed to create rollup sequencer manager contract instance", "err", err)
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
	consensusMessage, err := espresso_types.UnmarshalConsensusMessage(message)
	if err != nil {
		log.Error("Failed to unmarshal consensus message:", err)
		return err
	}
	if consensusMessage.Event.QuorumProposalWrapper != nil {
		return listener.processQuorumProposalEvent(consensusMessage.Event.QuorumProposalWrapper)
	} else if consensusMessage.Event.DaProposalWrapper != nil {
		return listener.processDaProposalEvent(consensusMessage.Event.DaProposalWrapper)
	} else if consensusMessage.Event.Decide != nil {
		return listener.processDecideEvent(consensusMessage.Event.Decide)
	}

	return nil
}

func (listener *HotshotListener) processQuorumProposalEvent(quorumProposalWrapper *espresso_types.QuorumProposalWrapper) error {
	log.Info("Received quorum proposal event", "event", quorumProposalWrapper)

	viewNumber := quorumProposalWrapper.QuorumProposalDataWrapper.Data.Proposal.ViewNumber
	builderCommitment := quorumProposalWrapper.QuorumProposalDataWrapper.Data.Proposal.BlockHeader.Fields.BuilderCommitment

	hexViewNumber := hexutil.Uint(viewNumber)

	// Combine the hexViewNumber and builderCommitment to get the key
	key := hexViewNumber.String() + builderCommitment

	l1FinalizedBlockNumberForView := quorumProposalWrapper.QuorumProposalDataWrapper.Data.Proposal.BlockHeader.Fields.L1Finalized.Number
	l1FinalizedBlockNumberBigInt := big.NewInt(int64(l1FinalizedBlockNumberForView))
	// Store the finalized L1 block number in the map
	listener.quorumViewNumberBuilderCommitment[key] = *l1FinalizedBlockNumberBigInt

	// Check if a da commitment exists for the key relative to
	// this quorum proposal view number and builder commitment
	if _, ok := listener.daViewNumberBuilderCommitment[key]; !ok {
		// If it does, then we can assume that this is a DA proposal

		log.Info("Waiting for Da proposal for the given builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)
		return nil
	}
	log.Info("Processing builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)

	// Get the sequencer address for the next view
	nextView := viewNumber + 1
	// Note: Its important to use l1 finalized block number here because we want the GetCurrentSequencer to
	// always return the same sequencer address for the same view number
	sequencerAddressForNextView, err := listener.rollupSequencerManager.GetCurrentSequencer(&bind.CallOpts{
		BlockNumber: l1FinalizedBlockNumberBigInt,
	}, big.NewInt(int64(nextView)))
	if err != nil {
		log.Error("Failed to get current sequencer", "err", err)
		return err
	}

	if sequencerAddressForNextView.Hex() == listener.sequencerAddress {
		log.Info("Next view is this node's view", "nextView", nextView, "sequencerAddress", listener.sequencerAddress)
		// TODO: Processing will be implemented in the next PR
	}

	// TODO: Processing will be implemented in the next PR

	// Delate the quorum and da proposal keys from the map
	// so that map doesnt take a lot of space in memory
	delete(listener.quorumViewNumberBuilderCommitment, key)
	delete(listener.daViewNumberBuilderCommitment, key)
	return nil
}

func (listener *HotshotListener) processDaProposalEvent(daProposalWrapper *espresso_types.DaProposalWrapper) error {
	log.Info("Recieved DA Proposal event", "event", daProposalWrapper)

	// Now get the view number for the given builder commitment
	viewNumber := daProposalWrapper.DaProposalDataWrapper.Data.ViewNumber
	// Convert the viewNumber to a hex string
	hexViewNumber := hexutil.Uint(viewNumber)

	blockPayload, err := espresso_types.NewBlockPayload(daProposalWrapper.DaProposalDataWrapper.Data.EncodedTransactions,
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
		log.Error("Failed to convert builder commitment to tagged string:", err)
		return err
	}

	key := hexViewNumber.String() + builderCommitmentString

	// Now store the key and check if a quorum proposal exists for the given builder commitment
	listener.daViewNumberBuilderCommitment[key] = true
	// Check if a da commitment exists for this key
	// relative to this DA proposal view number and builder commitment
	if _, ok := listener.quorumViewNumberBuilderCommitment[key]; !ok {
		// If it does, then we can assume that this is a DA proposal
		log.Info("Waiting for Quorum proposal for the given builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitmentString)
		return nil
	}

	// Process the DA proposal and quorum proposal
	log.Info("Processing builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitmentString)

	// Get L1 block number from the quorum proposal map
	l1FinalizedBlockNumberForView := listener.quorumViewNumberBuilderCommitment[key]
	nextView := viewNumber + 1
	// Note: Its important to use l1 finalized block number here because we want the GetCurrentSequencer to
	// always return the same sequencer address for the same view number
	sequencerAddressForNextView, err := listener.rollupSequencerManager.GetCurrentSequencer(&bind.CallOpts{
		BlockNumber: &l1FinalizedBlockNumberForView,
	}, big.NewInt(int64(nextView)))
	if err != nil {
		log.Error("Failed to get current sequencer", "err", err)
		return err
	}

	// Check if the sequencer address is the same address of this node
	if sequencerAddressForNextView.Hex() != listener.sequencerAddress {
		log.Info("Next view is not this node's view")
		// TODO: Processing will be implemented in the next PR
	}

	// TODO: Processing will be implemented in the next PR

	// Delate the quorum and da proposal keys from the map
	// so that map doesnt take a lot of space in memory
	delete(listener.quorumViewNumberBuilderCommitment, key)
	delete(listener.daViewNumberBuilderCommitment, key)
	return nil

}

func (listener *HotshotListener) processDecideEvent(decide *espresso_types.Decide) error {
	log.Info("Received Decide event", "event", decide)
	for _, leafChain := range decide.LeafChain {
		// Check if any of the leafs match the view number + builder commitment that we have stored
		viewNumber := leafChain.Leaf.ViewNumber
		builderCommitment := leafChain.Leaf.BlockHeader.Fields.BuilderCommitment
		log.Info("Processing leaf chain", "leafChain", leafChain, "builderCommitment", builderCommitment, "viewNumber", viewNumber)
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
				return
			}
			listener.processMessage(message)
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
