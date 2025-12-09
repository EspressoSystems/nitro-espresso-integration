package decentralized_timeboost_batch_verifier

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/espressostreamer"
	"github.com/offchainlabs/nitro/solgen/go/decentralizedtimeboostgen"
	"github.com/offchainlabs/nitro/util/signature"
)

type BatchPosterArgs struct {
	SignedData []byte `json:"signedData"`
	Hash       []byte `json:"hash"`
	PubKey     []byte `json:"pubKey"`
	Signature  []byte `json:"signature"`
}

type SigningData struct {
	SequencerNumber          uint64
	AfterDelayedMessagesRead uint64
	GasRefunder              common.Address
	PreviousMessageCount     arbutil.MessageIndex
	NewMessageCount          arbutil.MessageIndex
	Data                     []byte
}

type BatchRpcResponse struct {
	Response string        `json:"response"`
	Result   []byte        `json:"result,omitempty"`
	Error    *BatcherError `json:"error,omitempty"`
	Id       interface{}   `json:"id"`
}

type BatcherError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type VerifiedInfo struct {
	MessageCount  arbutil.MessageIndex
	HotshotHeight uint64
}

type BatchVerifier struct {
	LatestVerified       *VerifiedInfo
	publicKey            *ecdsa.PublicKey
	client               *http.Client
	timeboostKeyManager  *decentralizedtimeboostgen.KeyManager
	currentBatch         uint64
	lastBatchUpdatedTime time.Time
	leaderTimeouts       uint64
	signer               signature.DataSignerFunc
	waitForLeaderDelay   time.Duration
}

type BatchVerifierConfig struct {
	RpcTimeout          time.Duration `koanf:"rpc-timeout"`
	RpcKeepalive        time.Duration `koanf:"rpc-keepalive"`
	WaitForLeaderDelay  time.Duration `koanf:"wait-for-leader-delay"`
	MaxIdleConnsPerHost int           `koanf:"max-idle-conns-per-host"`
}

var DefaultBatchVerifierConfig = BatchVerifierConfig{
	RpcTimeout:          time.Second * 10,
	RpcKeepalive:        time.Second * 30,
	MaxIdleConnsPerHost: 5,
	WaitForLeaderDelay:  time.Minute * 5,
}

func DecentralizedTimeboostBatchVerifierConfigAddOptions(prefix string, f *pflag.FlagSet) {
	f.Duration(prefix+".rpc-timeout", DefaultBatchVerifierConfig.RpcTimeout, "timeout for http client")
	f.Duration(prefix+".rpc-keepalive", DefaultBatchVerifierConfig.RpcKeepalive, "keep alive for http client")
	f.Duration(prefix+".wait-for-leader-delay", DefaultBatchVerifierConfig.WaitForLeaderDelay, "how long we should wait for a leader to send batch, before trying constructing our own")
	f.Int(prefix+".max-idle-conns-per-host", DefaultBatchVerifierConfig.MaxIdleConnsPerHost, "max idle connections")
}

func NewBatchVerifier(
	config BatchVerifierConfig,
	timeboostKeyManager *decentralizedtimeboostgen.KeyManager,
	privKey string,
) (*BatchVerifier, error) {
	privKeyBytes, err := hex.DecodeString(privKey)
	if err != nil {
		return nil, err
	}
	privateKey, err := crypto.ToECDSA(privKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create ECDSA private key: %w", err)
	}
	signer := signature.DataSignerFromPrivateKey(privateKey)
	message := make([]byte, 32)
	signature, err := signer(message)
	if err != nil {
		return nil, err
	}

	publicKey, err := crypto.SigToPub(message, signature)
	if err != nil {
		return nil, err
	}
	return &BatchVerifier{
		publicKey: publicKey,
		client: &http.Client{
			Timeout: config.RpcTimeout,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: int(config.MaxIdleConnsPerHost),
				DialContext: (&net.Dialer{
					Timeout:   config.RpcTimeout,
					KeepAlive: config.RpcKeepalive,
				}).DialContext,
				Dial: func(network, addr string) (net.Conn, error) {
					return net.Dial(network, addr)
				},
			},
		},
		timeboostKeyManager:  timeboostKeyManager,
		currentBatch:         0,
		lastBatchUpdatedTime: time.Now(),
		leaderTimeouts:       0,
		signer:               signer,
		waitForLeaderDelay:   config.WaitForLeaderDelay,
	}, nil
}

func (v *BatchVerifier) getAbiArguments() (abi.Arguments, error) {
	bytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create bytes type: %w", err)
	}
	uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create uint256 type: %w", err)
	}
	addressType, err := abi.NewType("address", "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create address type: %w", err)
	}
	return abi.Arguments{
		{Name: "sequencerNumber", Type: uint256Type},
		{Name: "data", Type: bytesType},
		{Name: "afterDelayedMessagesRead", Type: uint256Type},
		{Name: "gasRefunder", Type: addressType},
		{Name: "prevMessageCount", Type: uint256Type},
		{Name: "newMessageCount", Type: uint256Type},
	}, nil
}

func (v *BatchVerifier) sendBatchForVerification(
	args *BatchPosterArgs,
	members []decentralizedtimeboostgen.KeyManagerCommitteeMember,
) ([]byte, error) {
	requiredQuorum := (2*len(members))/3 + 1
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "batcher_submitBatch",
		"params":  []BatchPosterArgs{*args},
		"id":      1,
	}
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("batch poster failed to marshal JSON: %w", err)
	}

	var sigs [][]byte
	v.adjustRecoveryByte(args.Signature)
	// account for our own
	sigCount := 1
	// Note: We append empty signatures on any error because if we still receive a quorum of signatures,
	// we will still try to post the batch and timeboost contracts checks signatures in order in respect to member ordering in contract
	for _, member := range members {
		if bytes.Equal(member.SigKey, v.getCompressedPubKey()) {
			// we created the batch, no need to send it to ourselves, append our signature
			sigs = append(sigs, args.Signature)
			continue
		}
		addr := strings.TrimSpace(member.BatchPosterAddress)
		if !strings.HasPrefix(addr, "http://") {
			addr = "http://" + addr
		}
		resp, err := v.client.Post(addr, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			log.Error("http request failed", "err", err, "to", member.SigKey)
			sigs = append(sigs, []byte{})
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Error("server returned returned error", "error code", resp.StatusCode)
			sigs = append(sigs, []byte{})
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Error("failed to read response body", "err", err, "url", member.BatchPosterAddress)
			sigs = append(sigs, []byte{})
			continue
		}
		var rpcResponse BatchRpcResponse
		if err := json.Unmarshal(body, &rpcResponse); err != nil {
			log.Error("failed to unmarshal json response", "err", err, "url", member.BatchPosterAddress)
			sigs = append(sigs, []byte{})
			continue
		}
		if rpcResponse.Error != nil {
			log.Error("got error from request", "err", rpcResponse.Error.Message, "url", member.BatchPosterAddress)
			sigs = append(sigs, []byte{})
			continue
		}

		pubKey, err := crypto.SigToPub(args.Hash, rpcResponse.Result)
		if err != nil {
			log.Error("failed to recover public key", "err", err, "url", member.BatchPosterAddress)
			sigs = append(sigs, []byte{})
			continue
		}

		if !bytes.Equal(crypto.CompressPubkey(pubKey), member.SigKey) {
			log.Error("failed to validate signature in the committee", "url", member.BatchPosterAddress)
			sigs = append(sigs, []byte{})
			continue
		}
		v.adjustRecoveryByte(rpcResponse.Result)
		sigs = append(sigs, rpcResponse.Result)
		sigCount += 1
	}
	if sigCount < requiredQuorum {
		return nil, fmt.Errorf("did not receive enough valid signatures for batch correctness. wanted: %d, have: %d", requiredQuorum, sigCount)
	}
	bytesType, err := abi.NewType("bytes[]", "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create bytes array type: %w", err)
	}
	arguments := abi.Arguments{
		{
			Type: bytesType,
		},
	}
	encodedSigs, err := arguments.Pack(sigs)
	if err != nil {
		return nil, fmt.Errorf("failed to ABI encode signatures: %w", err)
	}
	log.Info("decentralized timeboost received enough signatures", "signatures received", sigCount, "quorum", requiredQuorum)
	return encodedSigs, nil
}

func (v *BatchVerifier) adjustRecoveryByte(sig []byte) {
	length := len(sig)
	if length > 0 {
		// Get the last byte (v)
		vIndex := length - 1
		v := sig[vIndex]

		// Adjusting ECDSA signature 'v' value for Ethereum compatibility
		// Get `v` from the signature and verify the byte is in expected format for openzeppelin `ECDSA.recover`
		// https://github.com/ethereum/go-ethereum/issues/19751
		if v == 0 || v == 1 {
			sig[vIndex] = v + 27
		}
	}
}

func (v *BatchVerifier) getCompressedPubKey() []byte {
	return crypto.CompressPubkey(v.publicKey)
}

func (v *BatchVerifier) IsLeaderForBatch(seqNum uint64, msgCount arbutil.MessageIndex, batchMsgCount arbutil.MessageIndex) (bool, error) {
	// reset if we have received a batch from inbox contract, or if there are no new messages
	if seqNum != v.currentBatch || msgCount <= batchMsgCount {
		v.currentBatch = seqNum
		v.lastBatchUpdatedTime = time.Now()
		v.leaderTimeouts = 0
	}
	id, err := v.timeboostKeyManager.CurrentCommitteeId(&bind.CallOpts{})
	if err != nil {
		return false, err
	}
	committee, err := v.timeboostKeyManager.GetCommitteeById(&bind.CallOpts{}, id)
	if err != nil {
		return false, err
	}

	pubKey := v.getCompressedPubKey()
	if time.Since(v.lastBatchUpdatedTime) >= v.waitForLeaderDelay {
		v.lastBatchUpdatedTime = time.Now()
		v.leaderTimeouts += 1
		log.Warn(
			"time expired waiting for batch to be posted from leader",
			"leader timeouts", v.leaderTimeouts,
			"batch num", seqNum,
		)
	}
	leader := committee.Members[(seqNum+v.leaderTimeouts)%uint64(len(committee.Members))]
	if !bytes.Equal(pubKey, leader.SigKey) {
		return false, nil
	}

	return true, nil
}

func (v *BatchVerifier) HashBatchData(data []byte) []byte {
	return crypto.Keccak256Hash(data).Bytes()
}

func (v *BatchVerifier) HashAndSignBatchData(
	signingData []byte,
) (*BatchPosterArgs, error) {
	hash := v.HashBatchData(signingData)
	signature, err := v.signer(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to sign data: %w", err)
	}
	args := &BatchPosterArgs{
		SignedData: signingData,
		Signature:  signature,
		Hash:       hash,
		PubKey:     crypto.CompressPubkey(v.publicKey),
	}
	return args, nil
}

func (v *BatchVerifier) VerifySignatureOverHash(hash []byte, signature []byte, publicKeyBytes []byte) error {
	pubKey, err := crypto.SigToPub(hash, signature)
	if err != nil {
		return fmt.Errorf("failed to recover public key: %w", err)
	}

	recoveredPubKeyBytes := crypto.CompressPubkey(pubKey)

	if !bytes.Equal(recoveredPubKeyBytes, publicKeyBytes) {
		return fmt.Errorf("recovered pub key doesnt match sent pub key. got: 0x%s, wanted: 0x%s", hex.EncodeToString(publicKeyBytes), hex.EncodeToString(recoveredPubKeyBytes))
	}

	return nil
}

func (v *BatchVerifier) GetBlobAbiArguments() (abi.Arguments, error) {
	bytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create bytes type: %w", err)
	}
	uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create uint256 type: %w", err)
	}
	addressType, err := abi.NewType("address", "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create address type: %w", err)
	}
	return abi.Arguments{
		{Name: "sequencerNumber", Type: uint256Type},
		{Name: "afterDelayedMessagesRead", Type: uint256Type},
		{Name: "gasRefunder", Type: addressType},
		{Name: "prevMessageCount", Type: uint256Type},
		{Name: "newMessageCount", Type: uint256Type},
		{Name: "data", Type: bytesType},
	}, nil
}

func (v *BatchVerifier) GetSignedDataFromBytes(data []byte, blobs bool) (*SigningData, error) {
	var arguments abi.Arguments
	var err error
	if blobs {
		arguments, err = v.GetBlobAbiArguments()
	} else {
		arguments, err = v.getAbiArguments()
	}
	if err != nil {
		return nil, err
	}
	unpackedMap := make(map[string]interface{})
	err = arguments.UnpackIntoMap(unpackedMap, data)
	if err != nil {
		return nil, err
	}
	seqNum, ok := unpackedMap["sequencerNumber"].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("invalid sequencerNumber type: %v", unpackedMap["prevMessageCount"])
	}

	msgData, ok := unpackedMap["data"].([]byte)
	if !ok {
		return nil, fmt.Errorf("invalid msg data type: %v", unpackedMap["data"])
	}

	delayedMsgs, ok := unpackedMap["afterDelayedMessagesRead"].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("invalid delayed messages type: %v", unpackedMap["afterDelayedMessagesRead"])
	}

	address, ok := unpackedMap["gasRefunder"].(common.Address)
	if !ok {
		return nil, fmt.Errorf("invalid address type: %v", unpackedMap["gasRefunder"])
	}

	prevMsg, ok := unpackedMap["prevMessageCount"].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("invalid prev msg type: %v", unpackedMap["prevMessageCount"])
	}

	newMsg, ok := unpackedMap["newMessageCount"].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("invalid new msg type: %v", unpackedMap["newMessageCount"])
	}
	return &SigningData{
		SequencerNumber:          seqNum.Uint64(),
		Data:                     msgData,
		AfterDelayedMessagesRead: delayedMsgs.Uint64(),
		GasRefunder:              address,
		PreviousMessageCount:     arbutil.MessageIndex(prevMsg.Uint64()),
		NewMessageCount:          arbutil.MessageIndex(newMsg.Uint64()),
	}, nil
}

func (v *BatchVerifier) VerifySignedDataCorrectness(
	signedData *SigningData,
	seqNum uint64,
	gasRefundAddr common.Address,
	messageCount arbutil.MessageIndex,
	args BatchPosterArgs,
	encodedBlobs []byte,
	streamer *espressostreamer.EspressoStreamer,
) error {
	if signedData.SequencerNumber != seqNum {
		return fmt.Errorf("failed to match seq num. got %d, wanted %d", signedData.SequencerNumber, seqNum)
	}
	if signedData.GasRefunder != gasRefundAddr {
		return fmt.Errorf("failed to match gas refunder. got %d, wanted %d", signedData.GasRefunder, gasRefundAddr)
	}
	if signedData.PreviousMessageCount != messageCount {
		return fmt.Errorf("failed to match previous message count. got %d, wanted %d", signedData.PreviousMessageCount, messageCount)
	}

	// We need to verify we have indeed received the transactions from espresso
	hotshotHeight := streamer.VerifyConsecutivePositions(uint64(signedData.PreviousMessageCount), uint64(signedData.NewMessageCount-1))
	if hotshotHeight == nil {
		return fmt.Errorf("failed to match new message data vs whats in streamer. wanted: %d", signedData.NewMessageCount)
	}
	if *hotshotHeight != uint64(math.MaxUint64) {
		v.LatestVerified = &VerifiedInfo{
			MessageCount:  signedData.NewMessageCount,
			HotshotHeight: *hotshotHeight,
		}
	}

	var calldata []byte
	if len(encodedBlobs) > 0 {
		arguments, err := v.GetBlobAbiArguments()
		if err != nil {
			return err
		}
		calldata, err = arguments.Pack(
			new(big.Int).SetUint64(signedData.SequencerNumber),
			new(big.Int).SetUint64(signedData.AfterDelayedMessagesRead),
			signedData.GasRefunder,
			new(big.Int).SetUint64(uint64(signedData.PreviousMessageCount)),
			new(big.Int).SetUint64(uint64(signedData.NewMessageCount)),
			encodedBlobs,
		)
		if err != nil {
			return err
		}
	} else {
		arguments, err := v.getAbiArguments()
		if err != nil {
			return err
		}
		calldata, err = arguments.Pack(
			new(big.Int).SetUint64(signedData.SequencerNumber),
			signedData.Data,
			new(big.Int).SetUint64(signedData.AfterDelayedMessagesRead),
			signedData.GasRefunder,
			new(big.Int).SetUint64(uint64(signedData.PreviousMessageCount)),
			new(big.Int).SetUint64(uint64(signedData.NewMessageCount)),
		)
		if err != nil {
			return err
		}
	}

	hash := v.HashBatchData(calldata)
	if !bytes.Equal(hash, args.Hash) {
		return fmt.Errorf("failed to verify hash, calculated hash. calculated: 0x%s, received: 0x:%s", hex.EncodeToString(hash), hex.EncodeToString(args.Hash))
	}

	if err := v.VerifySignatureOverHash(args.Hash, args.Signature, args.PubKey); err != nil {
		return fmt.Errorf("failed to verify signature: %w", err)
	}
	return nil
}

func (v *BatchVerifier) SignAndSendBatchIfLeader(
	arguments abi.Arguments,
	seqNum *big.Int,
	l2MessageData []byte,
	delayedMsg *big.Int,
	gasRefunder common.Address,
	prevMsgNum *big.Int,
	newMsgNum *big.Int,
) ([]byte, error) {
	id, err := v.timeboostKeyManager.CurrentCommitteeId(&bind.CallOpts{})
	if err != nil {
		return nil, err
	}
	committee, err := v.timeboostKeyManager.GetCommitteeById(&bind.CallOpts{}, id)
	if err != nil {
		return nil, err
	}

	calldata, err := arguments.Pack(
		seqNum,
		l2MessageData,
		delayedMsg,
		gasRefunder,
		prevMsgNum,
		newMsgNum,
	)
	if err != nil {
		return nil, err
	}

	args, err := v.HashAndSignBatchData(calldata)
	if err != nil {
		return nil, err
	}

	sigs, err := v.sendBatchForVerification(
		args,
		committee.Members,
	)
	if err != nil {
		return nil, err
	}
	return sigs, nil
}

func (v *BatchVerifier) SignAndSendBlobBatchIfLeader(
	seqNum *big.Int,
	l2MessageData []byte,
	delayedMsg *big.Int,
	gasRefunder common.Address,
	prevMsgNum *big.Int,
	newMsgNum *big.Int,
	encodedBlobs []byte,
) ([]byte, error) {
	id, err := v.timeboostKeyManager.CurrentCommitteeId(&bind.CallOpts{})
	if err != nil {
		return nil, err
	}
	committee, err := v.timeboostKeyManager.GetCommitteeById(&bind.CallOpts{}, id)
	if err != nil {
		return nil, err
	}
	// We need to signed the encoded blobs, but send the message meta data for verification
	// First construct calldata with encoded blobs to be signed
	arguments, err := v.GetBlobAbiArguments()
	if err != nil {
		return nil, err
	}
	calldata, err := arguments.Pack(
		seqNum,
		delayedMsg,
		gasRefunder,
		prevMsgNum,
		newMsgNum,
		encodedBlobs,
	)
	if err != nil {
		return nil, err
	}

	// Sign encoded blobs calldata
	args, err := v.HashAndSignBatchData(calldata)
	if err != nil {
		return nil, err
	}

	// Repack with message data and send this to other nodes
	// Who will compute encoded blob hashes and sign
	calldata, err = arguments.Pack(
		seqNum,
		delayedMsg,
		gasRefunder,
		prevMsgNum,
		newMsgNum,
		l2MessageData,
	)
	if err != nil {
		return nil, err
	}
	args.SignedData = calldata
	sigs, err := v.sendBatchForVerification(
		args,
		committee.Members,
	)
	if err != nil {
		return nil, err
	}
	return sigs, nil
}

func (b *BatchVerifier) CheckLatestVerified(msgCount arbutil.MessageIndex, espressoStreamer *espressostreamer.EspressoStreamer) {
	hasVerified := b.LatestVerified != nil
	if !hasVerified || b.LatestVerified.MessageCount != msgCount {
		// This can be a case where leader sent us a batch, we verified it but they never posted to L1
		start := msgCount
		if hasVerified && b.LatestVerified.MessageCount < msgCount {
			start = b.LatestVerified.MessageCount
			log.Warn("last verified an old batch resetting", "last verified", start, "messageCount", msgCount)
		}
		b.LatestVerified = nil
		// See if message count is in streamer
		block := espressoStreamer.VerifyConsecutivePositions(uint64(start), uint64(msgCount))
		if block != nil && *block != uint64(math.MaxUint64) {
			log.Info(
				"no batch was yet verified but found correct starting position in espresso streamer",
				"messageCount", uint64(msgCount),
				"hotshot block", *block,
			)
			b.LatestVerified = &VerifiedInfo{
				MessageCount:  msgCount,
				HotshotHeight: *block,
			}
		}
	}
}

func (b *BatchVerifier) ShouldBuildBatch(ctx context.Context, msgCount arbutil.MessageIndex, espressoStreamer *espressostreamer.EspressoStreamer) bool {
	if b.LatestVerified == nil {
		// If we dont have the correct starting position in streamer and this in not the start of a chain
		// We need to wait for a quorum of nodes to post a batch so eventually we can catch up
		if msgCount > 1 {
			log.Warn("batch poster is yet to verify a batch. Waiting for batch verification before continuing", "messageCount", msgCount)
			return false
		}
	} else {
		// Look in the espresso streamer first to see if we need to reset or not
		// For timeboost to help speed things up, resetting may not be necessary
		// If a node has verified the previous batch go through the espresso streamer to the start message
		// This will also increase streamer current position, in the case that they were non-leader batch posters they should do so now
		found := false
		if b.LatestVerified.MessageCount == msgCount {
			// When other batch posters verify the streamer position they do not call `Advance()`
			// This is in case the leader never posts we do not need to reparse old hotshot blocks.
			// So now we can start advancing
			for {
				msg := espressoStreamer.Next(ctx)
				if msg == nil {
					break
				}
				if msg.Pos == uint64(msgCount-1) {
					log.Info("found next position in espresso streamer. no need for reset", "messageCount", msgCount)
					found = true
					break
				}
			}
		}
		if !found {
			log.Info(
				"resetting streamer to last verified",
				"messageCount", msgCount,
				"last verified", b.LatestVerified.MessageCount,
				"hotshot block", b.LatestVerified.HotshotHeight,
			)
			espressoStreamer.Reset(uint64(b.LatestVerified.MessageCount), uint64(b.LatestVerified.HotshotHeight))
		}
	}
	return true
}

func (b *BatchVerifier) LogTransactions(msg string, txns types.Transactions) {
	for _, txn := range txns {
		from, _ := types.Sender(types.LatestSignerForChainID(txn.ChainId()), txn)
		to := "<nil>"
		if txn.To() != nil {
			to = txn.To().Hex()
		}
		log.Warn(msg,
			"hash", txn.Hash().Hex(),
			"from", from.Hex(),
			"to", to,
			"time", txn.Time(),
			"valueEth", txn.Value(),
			"gas", txn.Gas(),
			"gwei", txn.GasPrice(),
			"nonce", txn.Nonce(),
			"data", txn.Data(),
		)
	}
}
