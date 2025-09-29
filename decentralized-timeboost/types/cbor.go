package decentralized_timeboost_types

import espressoCommon "github.com/EspressoSystems/espresso-network/sdks/go/types/common"

type Round struct {
	Number      uint64 `cbor:"0,keyasint"`
	CommitteeId uint64 `cbor:"1,keyasint"`
}

type Block struct {
	Number  uint64 `cbor:"0,keyasint"`
	Round   uint64 `cbor:"1,keyasint"`
	Payload []byte `cbor:"2,keyasint"`
}

type BlockInfo struct {
	Num   uint64 `cbor:"0,keyasint"`
	Round Round  `cbor:"1,keyasint"`
	Hash  []byte `cbor:"2,keyasint"`
}

type Certificate struct {
	Data       BlockInfo        `cbor:"0,keyasint"`
	Commitment []byte           `cbor:"1,keyasint"`
	Signatures map[uint8][]byte `cbor:"2,keyasint"`
}

type CertifiedBlock struct {
	Version uint8       `cbor:"0,keyasint"`
	Data    Block       `cbor:"1,keyasint"`
	Cert    Certificate `cbor:"2,keyasint"`
}

type Body struct {
	Blocks []CertifiedBlock `cbor:"0,keyasint"`
}

type MessagePayload struct {
	Position uint64 `cbor:"pos"`
	Message  []byte `cbor:"msg"`
}

func (r *Round) Commit() espressoCommon.Commitment {
	return espressoCommon.NewRawCommitmentBuilder("Round").
		Field("num", espressoCommon.NewRawCommitmentBuilder("Round Number Commitment").Uint64(r.Number).Finalize()).
		Field("com", espressoCommon.NewRawCommitmentBuilder("CommitteeId").Uint64(r.CommitteeId).Finalize()).
		Finalize()
}

func (b *BlockInfo) CommitNum() espressoCommon.Commitment {
	return espressoCommon.NewRawCommitmentBuilder("Block Number Commitment").
		Uint64(b.Num).
		Finalize()
}

func (b *BlockInfo) CommitHash() espressoCommon.Commitment {
	return espressoCommon.NewRawCommitmentBuilder("BlockHash").
		FixedSizeField("block-hash", b.Hash).
		Finalize()
}
