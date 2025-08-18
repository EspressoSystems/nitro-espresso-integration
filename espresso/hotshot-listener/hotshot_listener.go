package hotshot_listener

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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

func NewHotshotListener(hotshotUrl string) (*HotshotListener, error) {
	if hotshotUrl == "" {
		return nil, fmt.Errorf("hotshot url is empty, please provide a valid url")
	}
	return &HotshotListener{
		hotshotUrl: hotshotUrl,
	}, nil
}

func (listener *HotshotListener) processMessage(message []byte) error {
	// Convert message to ConsensusMessage
	var consensusMessage ConsensusMessage
	err := json.Unmarshal(message, &consensusMessage)
	if err != nil {
		log.Error("Failed to unmarshal message:", err)
		return err
	}

	if consensusMessage.Event.QuorumProposalWrapper != nil {
		log.Info("Received quorum proposal event", "event", consensusMessage.Event)
		listener.processQuorumProposalEvent(consensusMessage.Event.QuorumProposalWrapper)

	} else if consensusMessage.Event.DaProposalWrapper != nil {
		log.Info("Received DA proposal event", "event", consensusMessage.Event)
		listener.processDaProposalEvent(consensusMessage.Event.DaProposalWrapper)
	} else if consensusMessage.Event.Decide != nil {
		log.Info("Received Decide event", "event", consensusMessage.Event)
		listener.processDecideEvent(consensusMessage.Event.Decide)
	}

	return nil
}

func (listener *HotshotListener) processQuorumProposalEvent(quorumProposalWrapper *QuorumProposalWrapper) {
	log.Info("Received quorum proposal event", "event", quorumProposalWrapper)
	// Now we need to get the builder commitment and view number for this quorum proposal

	viewNumber := quorumProposalWrapper.QuorumProposalDataWrapper.Data.Proposal.ViewNumber
	builderCommitment := quorumProposalWrapper.QuorumProposalDataWrapper.Data.Proposal.BlockHeader.Fields.BuilderCommitment

	// Convert the viewNumber to a hex string
	hexViewNumber := hexutil.Uint(viewNumber)

	// Combine the hexViewNumber and builderCommitment to get the key
	key := hexViewNumber.String() + builderCommitment

	// Store the key in the map
	listener.quorumViewNumberBuilderCommitment[key] = true
	// Check if a da commitment exists for this key
	if _, ok := listener.daViewNumberBuilderCommitment[key]; !ok {
		// If it does, then we can assume that this is a DA proposal

		log.Debug("Waiting for Da proposal for the given builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)
		return
	}
	// Process the DA proposal and quorum proposal

	log.Debug("Processing DA proposal for the given builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)

	// Delate the quorum and da proposal keys from the map
	// so that map doesnt take a lot of space in memory
	delete(listener.quorumViewNumberBuilderCommitment, key)
	delete(listener.daViewNumberBuilderCommitment, key)
}

func (listener *HotshotListener) processDaProposalEvent(daProposalWrapper *DaProposalWrapper) error {
	log.Info("Recieved DA Proposal event", "event", daProposalWrapper)

	// Now get the view number for the given builder commitment
	viewNumber := daProposalWrapper.DaProposalDataWrapper.Data.ViewNumber
	// Convert the viewNumber to a hex string
	hexViewNumber := hexutil.Uint(viewNumber)
	log.Info("Processing DA proposal for the given builder commitment and view number", "viewNumber", viewNumber)

	blockPayload, err := NewBlockPayload(daProposalWrapper.DaProposalDataWrapper.Data.EncodedTransactions,
		daProposalWrapper.DaProposalDataWrapper.Data.Metadata)
	if err != nil {
		return err
	}
	// Get the builder commitment
	builderCommitment, err := blockPayload.BuilderCommitment()
	if err != nil {
		return err
	}

	log.Info("Builder commitment", "builderCommitment", builderCommitment)
	builderCommitmentString, err := blockPayload.ToTaggedSting()
	if err != nil {
		return err
	}
	log.Info("Builder commitment string", "builderCommitmentString", builderCommitmentString)

	// Create the key
	key := hexViewNumber.String() + builderCommitmentString

	// Now store the key and check if a quorum proposal exists for the given builder commitment
	listener.daViewNumberBuilderCommitment[key] = true
	// Check if a da commitment exists for this key
	if _, ok := listener.quorumViewNumberBuilderCommitment[key]; !ok {
		// If it does, then we can assume that this is a DA proposal
		log.Debug("Waiting for Da proposal for the given builder commitment and view number", "viewNumber", viewNumber, "builderCommitment", builderCommitment)
		return nil
	}
	return nil

}

func (listener *HotshotListener) processDecideEvent(decide *Decide) {
	log.Info("Recieved Decide event", "event", decide)
	for _, leafChain := range decide.LeafChain {
		log.Info("Processing leaf chain", "leafChain", leafChain)
		// Check if any of the leafs match the view number + builder commitment that we have stored
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
