package hotshot_listener

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"

	tagged_base64 "github.com/EspressoSystems/espresso-network/sdks/go/tagged-base64"
	espresso_types "github.com/EspressoSystems/espresso-network/sdks/go/types"
)

type ConsensusMessage struct {
	View  int   `json:"view_number"`
	Event Event `json:"event"`
}

type Event struct {
	QuorumProposalWrapper *QuorumProposalWrapper `json:"QuorumProposal"`
	DaProposalWrapper     *DaProposalWrapper     `json:"DaProposal"`
	ViewFinished          *ViewFinished          `json:"ViewFinished"`
	Decide                *Decide                `json:"Decide"`
}

type ViewFinished struct {
	ViewNumber int `json:"view_number"`
}

type Decide struct {
	LeafChain []LeafChain `json:"leaf_chain"`
}

type LeafChain struct {
	Leaf Leaf `json:"leaf"`
}

type Leaf struct {
	ViewNumber  int         `json:"view_number"`
	BlockHeader BlockHeader `json:"block_header"`
}

type QuorumProposalWrapper struct {
	QuorumProposalDataWrapper QuorumProposalDataWrapper `json:"proposal"`
	Sender                    string                    `json:"sender"`
}

type QuorumProposalDataWrapper struct {
	Data      QuorumProposalData `json:"data"`
	Signature string             `json:"signature"`
}

type QuorumProposalData struct {
	Proposal QuorumProposal `json:"proposal"`
}

type QuorumProposal struct {
	BlockHeader BlockHeader `json:"block_header"`
	ViewNumber  int         `json:"view_number"`
}

type BlockHeader struct {
	Fields Fields `json:"fields"`
}

type Fields struct {
	ChainConfig struct {
		ChainConfig ChainConfig `json:"chain_config"`
	} `json:"chain_config"`
	L1Finalized       L1Finalized `json:"l1_finalized"`
	PayloadCommitment string      `json:"payload_commitment"`
	BuilderCommitment string      `json:"builder_commitment"`
}

type ChainConfig struct {
	Left struct {
		ChainID string `json:"chain_id"`
	} `json:"Left"`
}

type L1Finalized struct {
	Number    int    `json:"number"`
	Timestamp string `json:"timestamp"`
	Hash      string `json:"hash"`
}

// / DA Proposal Structs ///
type DaProposalWrapper struct {
	DaProposalDataWrapper DaProposalDataWrapper `json:"proposal"`
	Sender                string                `json:"sender"`
}

type DaProposalDataWrapper struct {
	Data      DAProposalData `json:"data"`
	Signature string         `json:"signature"`
}

type DAProposalData struct {
	EncodedTransactions []byte   `json:"encoded_transactions"`
	ViewNumber          uint     `json:"view_number"`
	Metadata            Metadata `json:"metadata"`
}

type Metadata struct {
	Bytes string `json:"bytes"`
}

type BlockPayload struct {
	RawPayload []byte                 `json:"raw_payload"`
	NsTable    espresso_types.NsTable `json:"ns_table"`
	tag        string
}

// FromBytes creates a new BlockPayload from the give raw payload and ns table metadata
func NewBlockPayload(blockPayloadBytes []byte, metadata Metadata) (*BlockPayload, error) {
	var blockPayload BlockPayload

	blockPayload.RawPayload = blockPayloadBytes
	nsTableBytes, err := base64.StdEncoding.DecodeString(metadata.Bytes)
	if err != nil {
		return nil, err
	}

	nsTable := espresso_types.NsTable{
		Bytes: nsTableBytes,
	}
	blockPayload.NsTable = nsTable
	blockPayload.tag = "BUILDER_COMMITMENT"
	return &blockPayload, nil
}

type BuilderCommitment [32]byte

func (b *BlockPayload) BuilderCommitment() (*BuilderCommitment, error) {
	hash := sha256.New()

	// Get the bytes of the length of the ns table

	var le [8]byte
	binary.LittleEndian.PutUint64(le[:], uint64(len(b.RawPayload)))
	hash.Write(le[:])

	binary.LittleEndian.PutUint64(le[:], uint64(len(b.NsTable.Bytes)))
	hash.Write(le[:])
	binary.LittleEndian.PutUint64(le[:], uint64(len(b.NsTable.Bytes)))
	hash.Write(le[:])

	hash.Write(b.RawPayload)
	hash.Write(b.NsTable.Bytes)
	hash.Write(b.NsTable.Bytes)
	var builderCommitment BuilderCommitment
	copy(builderCommitment[:], hash.Sum(nil))
	return &builderCommitment, nil
}

func (b *BlockPayload) ToTaggedSting() (string, error) {
	builderCommitment, err := b.BuilderCommitment()
	if err != nil {
		return "", err
	}
	tagged, err := tagged_base64.New(b.tag, builderCommitment[:])
	if err != nil {
		return "", err
	}
	return tagged.String(), nil
}
