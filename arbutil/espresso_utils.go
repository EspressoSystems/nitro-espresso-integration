package arbutil

import (
	"bytes"
	"encoding/binary"
	"errors"
	"time"

	espressoTypes "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/ccoveille/go-safecast"
	"github.com/fxamacker/cbor/v2"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

const MAX_ATTESTATION_QUOTE_SIZE int = 4 * 1024
const LEN_SIZE int = 8
const INDEX_SIZE int = 8
const HEADER_LEN int = 8

type SubmittedEspressoTx struct {
	Hash        string
	Pos         []MessageIndex
	Payload     []byte
	SubmittedAt time.Time `rlp:"optional"`
}

type EspressoHeader struct {
	// Transaction type to parse payload
	TransactionType TransactionType `cbor:"0,keyasint"`
	// The payload length, excluding the header
	PayloadLength uint64 `cbor:"1,keyasint"`
}

type EspressoHeaderInfo struct {
	// Transaction type to parse payload
	TransactionType TransactionType
	// The length of the header
	HeaderLength uint64
}

type TransactionType uint8

const (
	BatchPosterSignedTxn   TransactionType = 0
	DecentralizedTimeboost TransactionType = 1
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

	quoteSizeBuf := make([]byte, LEN_SIZE)
	binary.BigEndian.PutUint64(quoteSizeBuf, uint64(len(quote)))
	// Put the signature first. That would help easier parsing.
	result := quoteSizeBuf
	result = append(result, quote...)
	result = append(result, unsigned...)

	// Prepare the header
	header := EspressoHeader{
		TransactionType: BatchPosterSignedTxn,
		PayloadLength:   uint64(len(result)),
	}

	encoded, err := cbor.Marshal(header)
	if err != nil {
		return nil, err
	}
	// Put the header at the beginning of the payload
	headerBuf := make([]byte, HEADER_LEN)
	binary.BigEndian.PutUint64(headerBuf, uint64(len(encoded)))
	headerBuf = append(headerBuf, encoded...)
	result = append(headerBuf, result...)

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

func ParseHotshotPayloadForHeader(tx []byte) *EspressoHeaderInfo {
	if len(tx) < HEADER_LEN {
		log.Warn("hotshot transaction is too small for a header")
		return nil
	}
	headerLength := uint64(HEADER_LEN)
	// Try and see if there is a header
	headerSize := binary.BigEndian.Uint64(tx[:headerLength])
	offset := headerLength + headerSize
	encoded := tx[headerLength:offset]
	var header EspressoHeader
	err := cbor.Unmarshal(encoded, &header)
	if err != nil {
		log.Warn("failed to decode header", "err", err)
		return nil
	}
	strippedTx := tx[offset:]
	// if the set header payload length matches the total rest of payload length it must be a header
	if uint64(len(strippedTx)) == header.PayloadLength {
		switch header.TransactionType {
		case BatchPosterSignedTxn:
			return &EspressoHeaderInfo{
				TransactionType: header.TransactionType,
				HeaderLength:    offset,
			}
		case DecentralizedTimeboost:
			return &EspressoHeaderInfo{
				TransactionType: header.TransactionType,
				HeaderLength:    offset,
			}
		default:
			return nil
		}
	}
	return nil
}

func ParseHotShotPayload(payload []byte, header *EspressoHeaderInfo) (signature []byte, userDataHash []byte, indices []uint64, messages [][]byte, err error) {
	if header != nil {
		// parse the payload with no header
		payload = payload[header.HeaderLength:]
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
