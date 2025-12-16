package decentralized_timeboost_batch_verifier

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	HotshotBlock             uint64
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
	publicKey            *ecdsa.PublicKey
	client               *http.Client
	timeboostKeyManager  *decentralizedtimeboostgen.KeyManager
	currentBatch         uint64
	lastBatchUpdatedTime time.Time
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
		signer:               signer,
		waitForLeaderDelay:   config.WaitForLeaderDelay,
	}, nil
}

func (v *BatchVerifier) getBatchAbiArguments() (abi.Arguments, error) {
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
		{Name: "hotshotBlock", Type: uint256Type},
	}, nil
}

func (v *BatchVerifier) getBlobAbiArguments() (abi.Arguments, error) {
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
		{Name: "hotshotBlock", Type: uint256Type},
	}, nil
}

func (v *BatchVerifier) sendBatchForVerification(
	args *BatchPosterArgs,
	members []decentralizedtimeboostgen.KeyManagerCommitteeMember,
	hotshotBlock *big.Int,
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
		resp, err := v.sendWithRetries(addr, jsonData, member.SigKey)
		if err != nil {
			log.Error("http request failed after max tries", "err", err, "to", member.SigKey)
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
		return nil, fmt.Errorf("did not receive enough valid signatures for batch correctness. quorum: %d, signatures received: %d", requiredQuorum, sigCount)
	}
	bytesType, err := abi.NewType("bytes[]", "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create bytes array type: %w", err)
	}
	uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create uint256 type: %w", err)
	}
	arguments := abi.Arguments{
		{
			Type: bytesType,
		},
		{
			Type: uint256Type,
		},
	}
	encodedSigs, err := arguments.Pack(sigs, hotshotBlock)
	if err != nil {
		return nil, fmt.Errorf("failed to ABI encode signatures: %w", err)
	}
	log.Info("decentralized timeboost received enough signatures", "signatures received", sigCount, "quorum", requiredQuorum)
	return encodedSigs, nil
}

func (v *BatchVerifier) sendWithRetries(addr string, data []byte, sigKey []byte) (*http.Response, error) {
	const max = 5
	var err error
	for range max {
		resp, err := v.client.Post(addr, "application/json", bytes.NewBuffer(data))
		if err != nil {
			log.Error("http request failed", "err", err, "to", sigKey, "addr", addr)
			continue
		}
		return resp, nil
	}
	return nil, err
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
	leader := committee.Members[seqNum%uint64(len(committee.Members))]
	if !bytes.Equal(pubKey, leader.SigKey) {
		if time.Since(v.lastBatchUpdatedTime) >= v.waitForLeaderDelay {
			log.Warn(
				"time expired waiting for batch to be posted from leader. will construct our own batch",
				"batch num", seqNum,
			)
		} else {
			return false, nil
		}
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

func (v *BatchVerifier) GetSignedDataFromBytes(data []byte, blobs bool) (*SigningData, error) {
	var arguments abi.Arguments
	var err error
	if blobs {
		arguments, err = v.getBlobAbiArguments()
	} else {
		arguments, err = v.getBatchAbiArguments()
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
	hotshotBlock, ok := unpackedMap["hotshotBlock"].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("invalid new msg type: %v", unpackedMap["hotshotBlock"])
	}
	return &SigningData{
		SequencerNumber:          seqNum.Uint64(),
		Data:                     msgData,
		AfterDelayedMessagesRead: delayedMsgs.Uint64(),
		GasRefunder:              address,
		PreviousMessageCount:     arbutil.MessageIndex(prevMsg.Uint64()),
		NewMessageCount:          arbutil.MessageIndex(newMsg.Uint64()),
		HotshotBlock:             hotshotBlock.Uint64(),
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
) ([]byte, error) {
	if signedData.SequencerNumber != seqNum {
		return nil, fmt.Errorf("failed to match seq num. got %d, wanted %d", signedData.SequencerNumber, seqNum)
	}
	if signedData.GasRefunder != gasRefundAddr {
		return nil, fmt.Errorf("failed to match gas refunder. got %d, wanted %d", signedData.GasRefunder, gasRefundAddr)
	}
	if signedData.PreviousMessageCount != messageCount {
		return nil, fmt.Errorf("failed to match previous message count. got %d, wanted %d", signedData.PreviousMessageCount, messageCount)
	}

	// We need to verify we have indeed received the transactions from espresso
	hotshotHeight, err := streamer.GetEarliestHotshotBlockForPosition(uint64(signedData.NewMessageCount - 1))
	if err != nil {
		return nil, fmt.Errorf("failed to get hotshot block number. newMsgCount: %d, err: %w", signedData.NewMessageCount, err)
	}
	if hotshotHeight != signedData.HotshotBlock {
		return nil, fmt.Errorf("failed to match hotshot height. got hotshot block: %d, have hotshot block: %d. newMsgCount: %d", signedData.HotshotBlock, hotshotHeight, signedData.NewMessageCount)
	}

	var calldata []byte
	if len(encodedBlobs) > 0 {
		arguments, err := v.getBlobAbiArguments()
		if err != nil {
			return nil, err
		}
		calldata, err = arguments.Pack(
			new(big.Int).SetUint64(signedData.SequencerNumber),
			new(big.Int).SetUint64(signedData.AfterDelayedMessagesRead),
			signedData.GasRefunder,
			new(big.Int).SetUint64(uint64(signedData.PreviousMessageCount)),
			new(big.Int).SetUint64(uint64(signedData.NewMessageCount)),
			encodedBlobs,
			new(big.Int).SetUint64(uint64(signedData.HotshotBlock)),
		)
		if err != nil {
			return nil, err
		}
	} else {
		arguments, err := v.getBatchAbiArguments()
		if err != nil {
			return nil, err
		}
		calldata, err = arguments.Pack(
			new(big.Int).SetUint64(signedData.SequencerNumber),
			signedData.Data,
			new(big.Int).SetUint64(signedData.AfterDelayedMessagesRead),
			signedData.GasRefunder,
			new(big.Int).SetUint64(uint64(signedData.PreviousMessageCount)),
			new(big.Int).SetUint64(uint64(signedData.NewMessageCount)),
			new(big.Int).SetUint64(uint64(signedData.HotshotBlock)),
		)
		if err != nil {
			return nil, err
		}
	}

	hash := v.HashBatchData(calldata)
	if !bytes.Equal(hash, args.Hash) {
		return nil, fmt.Errorf("failed to verify hash, calculated hash. calculated: 0x%s, received: 0x:%s", hex.EncodeToString(hash), hex.EncodeToString(args.Hash))
	}

	if err := v.VerifySignatureOverHash(args.Hash, args.Signature, args.PubKey); err != nil {
		return nil, fmt.Errorf("failed to verify signature: %w", err)
	}
	return calldata, nil
}

func (v *BatchVerifier) SignAndSendBatchIfLeader(
	seqNum *big.Int,
	l2MessageData []byte,
	delayedMsg *big.Int,
	gasRefunder common.Address,
	prevMsgNum *big.Int,
	newMsgNum *big.Int,
	hotshotBlock *big.Int,
) ([]byte, error) {
	id, err := v.timeboostKeyManager.CurrentCommitteeId(&bind.CallOpts{})
	if err != nil {
		return nil, err
	}
	committee, err := v.timeboostKeyManager.GetCommitteeById(&bind.CallOpts{}, id)
	if err != nil {
		return nil, err
	}

	arguments, err := v.getBatchAbiArguments()
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
		hotshotBlock,
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
		hotshotBlock,
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
	hotshotBlock *big.Int,
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
	arguments, err := v.getBlobAbiArguments()
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
		hotshotBlock,
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
		hotshotBlock,
	)
	if err != nil {
		return nil, err
	}
	args.SignedData = calldata
	sigs, err := v.sendBatchForVerification(
		args,
		committee.Members,
		hotshotBlock,
	)
	if err != nil {
		return nil, err
	}
	return sigs, nil
}

func (b *BatchVerifier) LogTransactions(msg string, txns types.Transactions) {
	for _, txn := range txns {
		from, _ := types.Sender(types.LatestSignerForChainID(txn.ChainId()), txn)
		to := "<nil>"
		if txn.To() != nil {
			to = txn.To().Hex()
		}
		log.Warn(msg,
			"txHash", txn.Hash().Hex(),
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
