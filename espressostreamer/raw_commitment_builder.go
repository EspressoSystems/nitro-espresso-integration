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

// We need to follow how timeboost calculates the commit which is using this repository:
// https://github.com/EspressoSystems/commit
func NewRawCommitmentBuilder(tag string) *RawCommitmentBuilder {
	hasher := sha3.NewLegacyKeccak256()
	builder := &RawCommitmentBuilder{hasher: hasher}
	return builder.constantStr(tag)
}

func (b *RawCommitmentBuilder) constantStr(s string) *RawCommitmentBuilder {
	b.hasher.Write([]byte(s))
	b.fixedSizeBytes(INVALID_UTF8)
	return b
}

func (b *RawCommitmentBuilder) fixedSizeBytes(data []byte) *RawCommitmentBuilder {
	b.hasher.Write(data)
	return b
}

func (b *RawCommitmentBuilder) U64(val uint64) *RawCommitmentBuilder {
	numBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(numBytes, val)
	b.fixedSizeBytes(numBytes)
	return b
}

func (b *RawCommitmentBuilder) FieldBlockNum(num uint64) *RawCommitmentBuilder {
	b.constantStr("num")
	numBuilder := NewRawCommitmentBuilder("Block Number Commitment")
	numCommitment := numBuilder.U64(num).Finalize()
	b.hasher.Write(numCommitment)
	return b
}

func (b *RawCommitmentBuilder) Round(num uint64) *RawCommitmentBuilder {
	b.constantStr("num")
	roundNumBuilder := NewRawCommitmentBuilder("Round Number Commitment")
	roundNumCommitment := roundNumBuilder.U64(num).Finalize()
	b.hasher.Write(roundNumCommitment)
	return b
}

func (b *RawCommitmentBuilder) Hash(hash []byte) *RawCommitmentBuilder {
	b.constantStr("block-hash")
	b.hasher.Write(hash)
	return b
}

func (b *RawCommitmentBuilder) CommitteeId(committeeId uint64) *RawCommitmentBuilder {
	b.constantStr("com")
	committeeBuilder := NewRawCommitmentBuilder("CommitteeId")
	committeeHash := committeeBuilder.U64(committeeId).Finalize()
	b.hasher.Write(committeeHash)
	return b
}

func (b *RawCommitmentBuilder) FieldRound(round gethexec.Round) *RawCommitmentBuilder {
	b.constantStr("round")
	roundBuilder := NewRawCommitmentBuilder("Round")
	numCommitment := roundBuilder.Round(round.Number).CommitteeId(round.CommitteeId).Finalize()
	b.hasher.Write(numCommitment)
	return b
}

func (b *RawCommitmentBuilder) FieldHash(hash []byte) *RawCommitmentBuilder {
	b.constantStr("hash")
	hashBuilder := NewRawCommitmentBuilder("BlockHash")
	hashCommitment := hashBuilder.Hash(hash).Finalize()
	b.hasher.Write(hashCommitment)
	return b
}

func (b *RawCommitmentBuilder) Finalize() []byte {
	h := b.hasher.Sum(nil)
	return h
}
