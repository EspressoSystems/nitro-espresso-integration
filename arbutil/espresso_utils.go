package arbutil

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	espressoTypes "github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/ccoveille/go-safecast"
	"github.com/minio/sha256-simd"
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

func HashDirParallel(root string) (string, error) {
	files, err := dirhash.DirFiles(root, "")

	if err != nil {
		return "", err
	}

	// Ignore all files like logs which we dont want to hash
	filesList := make([]string, 0, len(files))
	for _, f := range files {
		if shouldIgnore(f) {
			continue
		}
		if strings.Contains(f, "\n") {
			return "", errors.New("dirhash: filenames with newlines are not supported")
		}
		filesList = append(filesList, f)
	}

	// Sort for deterministic ordering
	sort.Strings(filesList)

	filesLength := len(filesList)

	hashes := make([][32]byte, filesLength)

	// Number of CPUs determine the number of workers for our files
	workers := runtime.NumCPU()

	// create a channel for each file
	filesToProcessJobs := make(chan int, filesLength)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errorProcessingFile error

	// Each worker should call processFile to process files from the channel
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go processFileForHashing(filesToProcessJobs, filesList, root, hashes, &wg, &mu, &errorProcessingFile)
	}

	for i := 0; i < filesLength; i++ {
		filesToProcessJobs <- i
	}

	// After the last sent value is returned, close all the channels
	close(filesToProcessJobs)
	// wait for all workers to finish
	wg.Wait()

	// if any worker encountered an error, return it
	if errorProcessingFile != nil {
		return "", errorProcessingFile
	}

	h := sha256.New()
	for i, file := range filesList {
		fmt.Fprintf(h, "%x  %s\n", hashes[i], file)
	}

	return "h1:" + base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

func processFileForHashing(filesToProcessJobs chan int, filesList []string, root string, hashes [][32]byte, wg *sync.WaitGroup, mu *sync.Mutex, errorProcessingFile *error) {
	defer wg.Done()
	fileBuffer := make([]byte, 512*1024) // 512 KB buffer

	for fileIndex := range filesToProcessJobs {
		fileName := filesList[fileIndex]
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(fileName)))
		if err != nil {
			mu.Lock()
			if *errorProcessingFile == nil {
				*errorProcessingFile = err
			}
			mu.Unlock()
			return
		}

		hasher := sha256.New()
		_, err = io.CopyBuffer(hasher, f, fileBuffer)
		if err != nil {
			mu.Lock()
			if *errorProcessingFile == nil {
				*errorProcessingFile = err
			}
			mu.Unlock()
			f.Close()
			return
		}
		err = f.Close()
		if err != nil {
			mu.Lock()
			if *errorProcessingFile == nil {
				*errorProcessingFile = err
			}
			mu.Unlock()
			return
		}

		var hashArr [32]byte
		copy(hashArr[:], hasher.Sum(nil))
		hashes[fileIndex] = hashArr
	}
}

// VerifySnapshot verifies the snapshot by first trying to verify the stored snapshot checksum
// using the key manager public key. If that fails, it falls back to verifying the snapshot
// checksum against the provided snapshotChecksum in the config. If the snapshot is verified using the
// config snapshot checksum, it deletes the existing AuthTags ancient store to prepare for
// new tags from the new enclave hash.
// It returns true if the auth tags need to be re-initialized (which occurs when
// the config snapshot checksum is used and there is no valid snapshot.txt file),
// false otherwise.
func VerifySnapshot(snapshotChecksum string, parentChainDir string, l2chainDataDir string, ancientDir string, privateKey *ecdsa.PrivateKey) (bool, error) {
	pubKey := &privateKey.PublicKey
	// Check if snapshot verification is required or not
	// Check if snapshot.txt file exists along with a valid snapshot_signature.txt
	path := filepath.Join(parentChainDir, "snapshot_verified.txt")
	snapshotVerifiedSignature, err := os.ReadFile(path)
	if err == nil {
		// Verify the signature
		err = VerifyMessage([]byte("snapshot verified"), snapshotVerifiedSignature, pubKey)
		if err != nil {
			return false, fmt.Errorf("failed to verify snapshot verified signature: %w", err)
		}
		log.Info("Snapshot has already been verified previously")
		return false, nil
	}

	sha256Hash, err := HashDirParallel(l2chainDataDir)
	if err != nil {
		return false, err
	}

	// Check if the snapshot hash matches the one in the config
	if snapshotChecksum != sha256Hash {
		return false, fmt.Errorf("snapshot hash mismatch, want: %s, got: %s", sha256Hash, snapshotChecksum)
	}
	log.Info("Snapshot hash matches", "hash", sha256Hash)

	// Here we are deleting the `AuthTags` ancient store because we want to replace it with new tags
	// from the new enclave hash. We cant just overwrite the existing tags because freezer doesnt allow
	// you to modify the tags of an existing freezer.
	tagFreezerDir := filepath.Join(ancientDir, "auth-tags")
	err = os.RemoveAll(tagFreezerDir)
	if err != nil {
		return false, fmt.Errorf("failed to delete authtag ancient store: %w", err)
	}

	err = StoreSnapshotVerified(parentChainDir, privateKey)
	if err != nil {
		return false, fmt.Errorf("failed to store snapshot verified signature: %w", err)
	}

	return true, nil
}

func StoreSnapshotVerified(parentChainDir string, privKey *ecdsa.PrivateKey) error {
	// Store the signature in snapshot_verified.txt to avoid re-verifying in future
	signature, err := SignMessage([]byte("snapshot verified"), privKey)
	if err != nil {
		return fmt.Errorf("failed to sign snapshot verified message: %w", err)
	}
	path := filepath.Join(parentChainDir, "snapshot_verified.txt")

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create snapshot verified file: %w", err)
	}
	defer file.Close()
	_, err = file.Write(signature)
	if err != nil {
		return fmt.Errorf("failed to write snapshot verified file: %w", err)
	}
	log.Info("Stored the snapshot verified signature in a file", "path", path)
	return nil
}

func SignMessage(message []byte, privKey *ecdsa.PrivateKey) ([]byte, error) {
	hash := crypto.Keccak256Hash(message)
	return crypto.Sign(hash.Bytes(), privKey)
}

func VerifyMessage(message []byte, signature []byte, pubKey *ecdsa.PublicKey) error {
	if pubKey == nil {
		return errors.New("public key is nil")
	}
	hash := crypto.Keccak256Hash(message)
	sigPublicKey, err := crypto.SigToPub(hash.Bytes(), signature)
	if err != nil {
		return fmt.Errorf("failed to recover public key from signature: %w", err)
	}
	sigAddress := crypto.PubkeyToAddress(*sigPublicKey)
	expectedAddress := crypto.PubkeyToAddress(*pubKey)
	if sigAddress != expectedAddress {
		return fmt.Errorf("signature verification failed: expected address %s, got %s", expectedAddress.Hex(), sigAddress.Hex())
	}
	return nil
}
