package espressostreamer

import (
	"encoding/binary"
	"hash"

	"golang.org/x/crypto/sha3"

	"github.com/offchainlabs/nitro/execution/gethexec"
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

func (b *RawCommitmentBuilder) FieldBlockNum(num uint64) *RawCommitmentBuilder {
	b.constantStr("num")
	b.hasher.Write(NewRawCommitmentBuilder("Block Number Commitment").u64(num).Finalize())
	return b
}

func (b *RawCommitmentBuilder) Round(num uint64) *RawCommitmentBuilder {
	b.constantStr("num")
	b.hasher.Write(NewRawCommitmentBuilder("Round Number Commitment").u64(num).Finalize())
	return b
}

func (b *RawCommitmentBuilder) Hash(hash []byte) *RawCommitmentBuilder {
	b.constantStr("block-hash")
	b.hasher.Write(hash)
	return b
}

func (b *RawCommitmentBuilder) CommitteeId(committeeId uint64) *RawCommitmentBuilder {
	b.constantStr("com")
	b.hasher.Write(NewRawCommitmentBuilder("CommitteeId").u64(committeeId).Finalize())
	return b
}

func (b *RawCommitmentBuilder) FieldRound(round gethexec.Round) *RawCommitmentBuilder {
	b.constantStr("round")
	b.hasher.Write(NewRawCommitmentBuilder("Round").Round(round.Number).CommitteeId(round.CommitteeId).Finalize())
	return b
}

func (b *RawCommitmentBuilder) FieldHash(hash []byte) *RawCommitmentBuilder {
	b.constantStr("hash")
	b.hasher.Write(NewRawCommitmentBuilder("BlockHash").Hash(hash).Finalize())
	return b
}

func (b *RawCommitmentBuilder) Finalize() []byte {
	return b.hasher.Sum(nil)
}
