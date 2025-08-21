package hotshot_listener

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"

	"github.com/gorilla/websocket"
)

const (
	HotshotListenerEndpoint = "/hotshot-events/events"
)

type HotshotListener struct {
	hotshotUrl                        string
	quorumViewNumberBuilderCommitment map[string]bool
	daViewNumberBuilderCommitment     map[string]bool
}

func NewHotshotListener(hotshotUrl string, rollupSequencerManagerContract string) (*HotshotListener, error) {

	if hotshotUrl == "" {
		return nil, fmt.Errorf("hotshot url is empty, please provide a valid url")
	}
	if rollupSequencerManagerContract == "" {
		return nil, fmt.Errorf("rollup sequencer manager contract address is empty, please provide a valid address")
	}

	// Create a new rollup sequencer manager contract instance
	return &HotshotListener{
		hotshotUrl: hotshotUrl,
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
		log.Debug("Received quorum proposal event", "event", consensusMessage.Event)
		listener.processQuorumProposalEvent(consensusMessage.Event.QuorumProposalWrapper)

	} else if consensusMessage.Event.DaProposalWrapper != nil {
		log.Debug("Received DA proposal event", "event", consensusMessage.Event)
		listener.processDaProposalEvent(consensusMessage.Event.DaProposalWrapper)
	} else if consensusMessage.Event.Decide != nil {
		log.Debug("Received Decide event", "event", consensusMessage.Event)
		listener.processDecideEvent(consensusMessage.Event.Decide)
	}

	return nil
}

func (listener *HotshotListener) processQuorumProposalEvent(quorumProposalWrapper *espresso_types.QuorumProposalWrapper) {
	log.Debug("Received quorum proposal event", "event", quorumProposalWrapper)

	viewNumber := quorumProposalWrapper.QuorumProposalDataWrapper.Data.Proposal.ViewNumber
	builderCommitment := quorumProposalWrapper.QuorumProposalDataWrapper.Data.Proposal.BlockHeader.Fields.BuilderCommitment

	hexViewNumber := hexutil.Uint(viewNumber)

	// Combine the hexViewNumber and builderCommitment to get the key
	key := hexViewNumber.String() + builderCommitment

	listener.quorumViewNumberBuilderCommitment[key] = true
	// Check if a da commitment exists for the key relative to
	// this quorum proposal view number and builder commitment
	if _, ok := listener.daViewNumberBuilderCommitment[key]; !ok {
		// If it does, then we can assume that this is a DA proposal

		log.Debug("Waiting for Da proposal for the given builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)
		return
	}
	// Process the DA proposal and quorum proposal
	log.Debug("Processing DA proposal for the given builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)
	// TODO: Processing will be implemented in the next PR

	// Delate the quorum and da proposal keys from the map
	// so that map doesnt take a lot of space in memory
	delete(listener.quorumViewNumberBuilderCommitment, key)
	delete(listener.daViewNumberBuilderCommitment, key)
}

func (listener *HotshotListener) processDaProposalEvent(daProposalWrapper *espresso_types.DaProposalWrapper) error {
	log.Info("Recieved DA Proposal event", "event", daProposalWrapper)

	// Now get the view number for the given builder commitment
	viewNumber := daProposalWrapper.DaProposalDataWrapper.Data.ViewNumber
	// Convert the viewNumber to a hex string
	hexViewNumber := hexutil.Uint(viewNumber)
	log.Info("Processing DA proposal for the given builder commitment and view number", "viewNumber", viewNumber)

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
		log.Debug("Waiting for Da proposal for the given builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)
		return nil
	}

	// Process the DA proposal and quorum proposal
	log.Debug("Processing DA proposal for the given builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)

	// TODO: Processing will be implemented in the next PR

	// Delate the quorum and da proposal keys from the map
	// so that map doesnt take a lot of space in memory
	delete(listener.quorumViewNumberBuilderCommitment, key)
	delete(listener.daViewNumberBuilderCommitment, key)
	return nil

}

func (listener *HotshotListener) processDecideEvent(decide *espresso_types.Decide) {
	log.Debug("Recieved Decide event", "event", decide)
	for _, leafChain := range decide.LeafChain {
		log.Info("Processing leaf chain", "leafChain", leafChain)
		// Check if any of the leafs match the view number + builder commitment that we have stored
		viewNumber := leafChain.Leaf.ViewNumber
		builderCommitment := leafChain.Leaf.BlockHeader.Fields.BuilderCommitment
		log.Debug("Processing leaf chain", "leafChain", leafChain, "builderCommitment", builderCommitment, "viewNumber", viewNumber)
		// TODO: Processing will be implemented in the next PR
	}
}

func (listener *HotshotListener) Start() error {
	conn, _, err := websocket.DefaultDialer.Dial(listener.hotshotUrl, nil)
	if err != nil {
		log.Error("failed to connect to hotshot webSocket", "err", err)
		return err
	}
	defer conn.Close()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}

			listener.processMessage(message)
		}
	}()
	<-interrupt
	os.Exit(0)

	return nil
}
