package arbutil

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	espressoTypes "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/ccoveille/go-safecast"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

const MAX_ATTESTATION_QUOTE_SIZE int = 4 * 1024
const LEN_SIZE int = 8
const INDEX_SIZE int = 8
const HEADER_SIZE = 1
const HEADER_LEN = 4

type SubmittedEspressoTx struct {
	Hash        string
	Pos         []MessageIndex
	Payload     []byte
	SubmittedAt time.Time `rlp:"optional"`
}

type Header struct {
	Version         TransactionVersion
	TransactionType TransactionType
	Reserved        uint16
}

type TransactionType uint8

const (
	Legacy       TransactionType = 0
	EphemeralKey TransactionType = 1
	Timeboost    TransactionType = 2
)

type TransactionVersion uint8

const (
	V0 TransactionVersion = 0
)

func BuildRawHotShotPayload(
	msgPositions []MessageIndex,
	msgFetcher func(MessageIndex) ([]byte, error),
	maxSize int64,
) ([]byte, int) {

	payload := []byte{}
	msgCnt := 0

	for _, p := range msgPositions {
		msgBytes, err := msgFetcher(p)
		if err != nil {
			log.Warn("failed to fetch the message", "pos", p)
			break
		}

		sizeBuf := make([]byte, LEN_SIZE)
		positionBuf := make([]byte, INDEX_SIZE)

		if len(payload)+len(sizeBuf)+len(msgBytes)+len(positionBuf)+MAX_ATTESTATION_QUOTE_SIZE > int(maxSize) {
			break
		}
		binary.BigEndian.PutUint64(sizeBuf, uint64(len(msgBytes)))
		binary.BigEndian.PutUint64(positionBuf, uint64(p))

		// Add the submitted txn position and the size of the message along with the message
		payload = append(payload, positionBuf...)
		payload = append(payload, sizeBuf...)
		payload = append(payload, msgBytes...)
		msgCnt += 1
	}
	return payload, msgCnt
}

func SignHotShotPayload(
	unsigned []byte,
	signer func([]byte) ([]byte, error),
) ([]byte, error) {
	quote, err := signer(unsigned)
	if err != nil {
		return nil, err
	}

	header := Header{
		Version:         V0,
		TransactionType: EphemeralKey,
		Reserved:        0,
	}

	encoded := []byte{
		uint8(header.Version),
		uint8(header.TransactionType),
		byte(header.Reserved >> 8),
		byte(header.Reserved),
	}

	headerBuf := make([]byte, HEADER_SIZE)
	size := len(encoded)
	if size > math.MaxUint8 {
		return nil, fmt.Errorf("encoded data too large: %d bytes (max %d)", len(encoded), math.MaxUint8)
	}
	headerBuf[0] = uint8(size)
	result := headerBuf
	result = append(result, encoded...)

	quoteSizeBuf := make([]byte, LEN_SIZE)
	binary.BigEndian.PutUint64(quoteSizeBuf, uint64(len(quote)))
	// Put the signature first. That would help easier parsing.
	result = append(result, quoteSizeBuf...)
	result = append(result, quote...)
	result = append(result, unsigned...)

	return result, nil
}

func ValidateIfPayloadIsInBlock(p []byte, payloads []espressoTypes.Bytes) bool {
	validated := false
	for _, payload := range payloads {
		if bytes.Equal(p, payload) {
			validated = true
			break
		}
	}
	return validated
}

func ParseHotshotPayloadForHeader(tx []byte) *TransactionType {
	if len(tx) < HEADER_SIZE+HEADER_LEN {
		log.Warn("hotshot transaction is too small for a header")
		return nil
	}
	// Try and see if there is a header
	size := tx[0]
	var transactionType TransactionType
	if size == HEADER_LEN {
		encoded := tx[HEADER_SIZE : HEADER_SIZE+HEADER_LEN]
		header := Header{
			Version:         TransactionVersion(encoded[0]),
			TransactionType: TransactionType(encoded[1]),
			Reserved:        binary.BigEndian.Uint16(encoded[2:4]),
		}

		if header.Version == V0 && header.Reserved == 0 {
			switch header.TransactionType {
			case Legacy:
				transactionType = Legacy
			case EphemeralKey:
				transactionType = EphemeralKey
			case Timeboost:
				transactionType = Timeboost
			default:
				return nil
			}
		}
	}
	return &transactionType
}

func ParseHotShotPayload(payload []byte, txType *TransactionType) (signature []byte, userDataHash []byte, indices []uint64, messages [][]byte, err error) {
	if txType != nil {
		payload = payload[HEADER_SIZE+HEADER_LEN:]
	}
	if len(payload) < LEN_SIZE {
		return nil, nil, nil, nil, errors.New("payload too short to parse signature size")
	}

	// Extract the signature size
	signatureSize, err := safecast.ToInt(binary.BigEndian.Uint64(payload[:LEN_SIZE]))
	if err != nil {
		return nil, nil, nil, nil, errors.New("could not convert signature size to int")
	}

	currentPos := LEN_SIZE

	if len(payload[currentPos:]) < signatureSize {
		return nil, nil, nil, nil, errors.New("payload too short for signature")
	}

	// Extract the signature
	signature = payload[currentPos : currentPos+signatureSize]
	currentPos += signatureSize

	indices = []uint64{}
	messages = [][]byte{}

	// Take keccak256 hash of the rest of payload
	userDataHash = crypto.Keccak256(payload[currentPos:])
	// Parse messages
	for {
		if currentPos == len(payload) {
			break
		}
		if len(payload[currentPos:]) < LEN_SIZE+INDEX_SIZE {
			return nil, nil, nil, nil, errors.New("remaining bytes")
		}

		// Extract the index
		index := binary.BigEndian.Uint64(payload[currentPos : currentPos+INDEX_SIZE])
		currentPos += INDEX_SIZE

		// Extract the message size
		messageSize, err := safecast.ToInt(binary.BigEndian.Uint64(payload[currentPos : currentPos+LEN_SIZE]))
		if err != nil {
			return nil, nil, nil, nil, errors.New("could not convert message size to int")
		}
		currentPos += LEN_SIZE

		if len(payload[currentPos:]) < messageSize {
			return nil, nil, nil, nil, errors.New("message size mismatch")
		}

		// Extract the message
		message := payload[currentPos : currentPos+messageSize]
		currentPos += messageSize
		if len(message) == 0 {
			// If the message has a size of 0, skip adding it to the list.
			continue
		}

		indices = append(indices, index)
		messages = append(messages, message)
	}

	return signature, userDataHash, indices, messages, nil
}
