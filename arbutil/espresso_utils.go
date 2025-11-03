package arbutil

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	espressoTypes "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/ccoveille/go-safecast"
	"golang.org/x/mod/sumdb/dirhash"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

const MAX_ATTESTATION_QUOTE_SIZE int = 4 * 1024
const LEN_SIZE int = 8
const INDEX_SIZE int = 8

type SubmittedEspressoTx struct {
	Hash        string
	Pos         []MessageIndex
	Payload     []byte
	SubmittedAt time.Time `rlp:"optional"`
}

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

func ParseHotShotPayload(payload []byte) (signature []byte, userDataHash []byte, indices []uint64, messages [][]byte, err error) {
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

var ignoreRE = []*regexp.Regexp{
	regexp.MustCompile(`(^|/)LOCK$`),
	regexp.MustCompile(`(^|/)FLOCK$`),
	regexp.MustCompile(`(^|/)CURRENT(\.bak)?$`),
	regexp.MustCompile(`(^|/)MANIFEST(-\d+)?$`),
	regexp.MustCompile(`(^|/)OPTIONS(-\d+)?$`),
	regexp.MustCompile(`(^|/)LOG(\.old)?$`),
	regexp.MustCompile(`(^|/)\d{6}\.log$`),
}

func shouldIgnore(rel string) bool {
	rel = strings.TrimPrefix(rel, "./")
	rel = strings.TrimSuffix(rel, "/")
	for _, re := range ignoreRE {
		if re.MatchString(rel) {
			return true
		}
		// also check just the base name for convenience
		if re.MatchString(path.Base(rel)) {
			return true
		}
	}
	return false
}

func HashDir(root string) (string, error) {
	files, err := dirhash.DirFiles(root, "")

	if err != nil {
		return "", err
	}

	// Exclude LOCK and FLOCK files
	out := make([]string, 0, len(files))
	for _, f := range files {
		if shouldIgnore(f) {
			continue
		}
		out = append(out, f)
	}

	// Hash (h1: base64(SHA-256)) of file contents
	return dirhash.Hash1(out, func(name string) (io.ReadCloser, error) {
		return os.Open(filepath.Join(root, filepath.FromSlash(name)))
	})
}

func VerifySnapshot(snapshotChecksum string, l2chainDataDir string, ancientDir string) error {
	sha256Hash, err := HashDir(l2chainDataDir)
	if err != nil {
		return err
	}

	// Check if the snapshot hash matches the one in the config
	if snapshotChecksum != sha256Hash {
		return fmt.Errorf("snapshot hash mismatch, want: %s, got: %s", sha256Hash, snapshotChecksum)
	}
	log.Info("Snapshot hash matches", "hash", sha256Hash)

	// Here we are deleting the `AuthTags` ancient store because we want to replace it with new tags
	// from the new enclave hash. We cant just overwrite the existing tags because freezer doesnt allow
	// you to modify the tags of an existing freezer.
	tagFreezerDir := filepath.Join(ancientDir, "auth-tags")
	err = os.RemoveAll(tagFreezerDir)
	if err != nil {
		return fmt.Errorf("failed to delete authtag ancient store: %w", err)
	}
	return nil
}
