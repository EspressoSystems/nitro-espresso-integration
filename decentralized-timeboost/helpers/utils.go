package decentralized_timeboost

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	espressoTypes "github.com/EspressoSystems/espresso-network/sdks/go/types"
	espressoCommon "github.com/EspressoSystems/espresso-network/sdks/go/types/common"
	"github.com/fxamacker/cbor/v2"
	"github.com/zeebo/blake3"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	decentralized_timeboost_types "github.com/offchainlabs/nitro/decentralized-timeboost/types"
	"github.com/offchainlabs/nitro/solgen/go/decentralizedtimeboostgen"
)

type DecentralizedTimeboostParsedMessage struct {
	Message arbostypes.MessageWithMetadata
	Pos     uint64
}

// Only one block version for now
const blockVersion = 1

// Get the timeboost calculated block hash
// See: https://github.com/EspressoSystems/timeboost/blob/ad534f3d7c6485e80b265811073d4e242dfd0746/timeboost-types/src/block.rs#L150-L155
func GetTimeboostBlockHash(round uint64, payload []byte) ([]byte, error) {
	hasher := blake3.New()
	roundBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(roundBytes, round)
	if _, err := hasher.Write(roundBytes); err != nil {
		return nil, fmt.Errorf("failed to write round to hasher: %w", err)
	}
	if _, err := hasher.Write(payload); err != nil {
		return nil, fmt.Errorf("failed to write payload to hasher: %w", err)
	}
	return hasher.Sum(nil), nil
}

// Validate the signatures in the timeboost generate certificate against the committee for one honest threshold
func ValidateTimeboostCertificate(
	commitment []byte,
	sigs map[uint8][]byte,
	members []decentralizedtimeboostgen.KeyManagerCommitteeMember,
) error {
	validSigs := 0
	oneHonestThreshold := (len(members)-1)/3 + 1
	validCert := false
	for keyId, sig := range sigs {
		member := members[int(keyId)]
		hasher := sha256.New()
		if _, err := hasher.Write(commitment); err != nil {
			return err
		}
		if !crypto.VerifySignature(member.SigKey, hasher.Sum(nil), sig) {
			// Continue through rest of signatures we need f + 1
			log.Warn("signature verification failed for key", "id", keyId)
			continue
		}
		validSigs += 1
		if validSigs >= oneHonestThreshold {
			validCert = true
			break
		}
	}
	if !validCert {
		return fmt.Errorf("not enough signatures found in certificate. wanted: %d have: %d", oneHonestThreshold, validSigs)
	}
	return nil
}

func ParseTimeboostEspressoTransaction(
	tx espressoTypes.Bytes,
	l1Height uint64,
	streamerCurrentPos uint64,
	timeboostKeyManager *decentralizedtimeboostgen.KeyManager,
) (*DecentralizedTimeboostParsedMessage, error) {
	var block decentralized_timeboost_types.CertifiedBlock
	if err := cbor.Unmarshal(tx, &block); err != nil {
		log.Warn("cbor error decoding certified block", "err", err)
		return nil, err
	}

	if block.Version != blockVersion {
		return nil, fmt.Errorf("block version mismatch! should be version 1 got %d", block.Version)
	}

	// We need to recalculate the `block hash` to ensure the data is the same
	// See: https://github.com/EspressoSystems/timeboost/blob/ad534f3d7c6485e80b265811073d4e242dfd0746/timeboost-types/src/block.rs#L150-L155
	blockHash, err := GetTimeboostBlockHash(block.Data.Round, block.Data.Payload)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(blockHash, block.Cert.Data.Hash) {
		return nil, fmt.Errorf("block hash mistmatch! computed hash: 0x%x, certified hash 0x%x", blockHash, block.Cert.Data.Hash)
	}

	// We need to ensure the commitment is the same between timeboost certificate and what is found in hotshot
	// See: https://github.com/EspressoSystems/timeboost/blob/ad534f3d7c6485e80b265811073d4e242dfd0746/timeboost-types/src/block.rs#L191-L197
	commitment := espressoCommon.NewRawCommitmentBuilder("BlockInfo").
		Field("num", block.Cert.Data.CommitNum()).
		Field("round", block.Cert.Data.Round.Commit()).
		Field("hash", block.Cert.Data.CommitHash()).
		Finalize()
	if !bytes.Equal(commitment[:], block.Cert.Commitment) {
		return nil, fmt.Errorf("block commitment mistmatch! computed commitment: 0x%x, certified commitment: 0x%x", commitment, block.Cert.Commitment)
	}

	// Validate the commitment against the committee signatures
	committee, err := timeboostKeyManager.GetCommitteeById(&bind.CallOpts{}, block.Cert.Data.Round.CommitteeId)
	if err != nil {
		log.Warn("failed to get committee", "committee id", block.Cert.Data.Round.CommitteeId, "err", err)
		return nil, err
	}
	if err = ValidateTimeboostCertificate(commitment[:], block.Cert.Signatures, committee.Members); err != nil {
		return nil, err
	}

	// After validation has succeeded deserialize the payload
	var msg decentralized_timeboost_types.MessagePayload
	if err = cbor.Unmarshal(block.Data.Payload, &msg); err != nil {
		log.Warn("cbor error decoding MessagePayload", "err", err)
		return nil, err
	}
	var messageWithMetadata arbostypes.MessageWithMetadata
	if err = rlp.DecodeBytes(msg.Message, &messageWithMetadata); err != nil {
		log.Warn("rlp error decoding MessagePayload to arbostypes.MessageWithMetadata", "err", err)
		return nil, err
	}

	if msg.Position < streamerCurrentPos {
		log.Warn("timeboost message index is less than current pos, skipping", "messageIndex", streamerCurrentPos, "currentMessagePos", msg.Position)
		return nil, nil
	}
	return &DecentralizedTimeboostParsedMessage{
		Message: messageWithMetadata,
		Pos:     msg.Position,
	}, nil
}
