package decentralized_timeboost

import (
	"encoding/binary"
	"hash"

	"golang.org/x/crypto/sha3"

	decentralized_timeboost_types "github.com/offchainlabs/nitro/decentralized-timeboost/types"
)

var INVALID_UTF8 = []byte{0xC0, 0x7F}

type RawCommitmentBuilder struct {
	hasher hash.Hash
}

// We need to follow how timeboost calculates the commitment which is using this repository:
// https://github.com/EspressoSystems/commit
func NewRawCommitmentBuilder(tag string) *RawCommitmentBuilder {
	builder := &RawCommitmentBuilder{hasher: sha3.NewLegacyKeccak256()}
	return builder.constantStr(tag)
}

func (b *RawCommitmentBuilder) constantStr(s string) *RawCommitmentBuilder {
	b.hasher.Write([]byte(s))
	return b.fixedSizeBytes(INVALID_UTF8)
}

func (b *RawCommitmentBuilder) fixedSizeBytes(data []byte) *RawCommitmentBuilder {
	b.hasher.Write(data)
	return b
}

func (b *RawCommitmentBuilder) u64(val uint64) *RawCommitmentBuilder {
	numBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(numBytes, val)
	return b.fixedSizeBytes(numBytes)
}

func (b *RawCommitmentBuilder) round(num uint64) *RawCommitmentBuilder {
	b.constantStr("num")
	b.hasher.Write(NewRawCommitmentBuilder("Round Number Commitment").u64(num).Finalize())
	return b
}

func (b *RawCommitmentBuilder) hash(hash []byte) *RawCommitmentBuilder {
	b.constantStr("block-hash")
	b.hasher.Write(hash)
	return b
}

func (b *RawCommitmentBuilder) committeeId(committeeId uint64) *RawCommitmentBuilder {
	b.constantStr("com")
	b.hasher.Write(NewRawCommitmentBuilder("CommitteeId").u64(committeeId).Finalize())
	return b
}

func (b *RawCommitmentBuilder) FieldBlockNum(num uint64) *RawCommitmentBuilder {
	b.constantStr("num")
	b.hasher.Write(NewRawCommitmentBuilder("Block Number Commitment").u64(num).Finalize())
	return b
}

func (b *RawCommitmentBuilder) FieldRound(round decentralized_timeboost_types.Round) *RawCommitmentBuilder {
	b.constantStr("round")
	b.hasher.Write(NewRawCommitmentBuilder("Round").round(round.Number).committeeId(round.CommitteeId).Finalize())
	return b
}

func (b *RawCommitmentBuilder) FieldHash(hash []byte) *RawCommitmentBuilder {
	b.constantStr("hash")
	b.hasher.Write(NewRawCommitmentBuilder("BlockHash").hash(hash).Finalize())
	return b
}

func (b *RawCommitmentBuilder) Finalize() []byte {
	return b.hasher.Sum(nil)
}
