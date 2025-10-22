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
	"time"

	"github.com/btcsuite/btcutil/base58"
	"github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/solgen/go/decentralizedtimeboostgen"
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

type BatchVerifier struct {
	privateKey *ecdsa.PrivateKey
	client     *http.Client
}

type BatchVerifierConfig struct {
	PrivateKey   string        `koanf:"private-key"`
	RpcTimeout   time.Duration `koanf:"rpc-timeout"`
	RpcKeepalive time.Duration `koanf:"rpc-keepalive"`
}

var DefaultBatchVerifierConfig = BatchVerifierConfig{
	PrivateKey:   "",
	RpcTimeout:   time.Second * 10,
	RpcKeepalive: time.Second * 30,
}

func DecentralizedTimeboostBatchVerifierConfigAddOptions(prefix string, f *pflag.FlagSet) {
	f.String(prefix+".private-key", DefaultBatchVerifierConfig.PrivateKey, "batch verifier private key")
	f.Duration(prefix+".rpc-timeout", DefaultBatchVerifierConfig.RpcTimeout, "timeout for http client")
	f.Duration(prefix+".rpc-keepalive", DefaultBatchVerifierConfig.RpcKeepalive, "keep alive for http client")
}

func NewBatchVerifier(config BatchVerifierConfig) (*BatchVerifier, error) {
	if len(config.PrivateKey) == 0 {
		return nil, fmt.Errorf("decentralized timeboost private key must be set")
	}
	decoded := base58.Decode(config.PrivateKey)
	privateKey, err := crypto.ToECDSA(decoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode decentralized timeboost private key: %w", err)
	}
	if privateKey == nil {
		return nil, fmt.Errorf("decentralized timeboost private key cannot be nil")
	}
	return &BatchVerifier{
		privateKey: privateKey,
		client: &http.Client{
			Timeout: config.RpcTimeout,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   config.RpcTimeout,
					KeepAlive: config.RpcKeepalive,
				}).DialContext,
				Dial: func(network, addr string) (net.Conn, error) {
					return net.Dial(network, addr)
				},
			},
		},
	}, nil
}

func (v *BatchVerifier) getCompressedPubKey() []byte {
	return crypto.CompressPubkey(&v.privateKey.PublicKey)
}

func (v *BatchVerifier) sendBatchForVerification(
	args *BatchPosterArgs,
	members []decentralizedtimeboostgen.KeyManagerCommitteeMember,
) ([]byte, error) {
	requiredQuorum := 2*(len(members)-1)/3 + 1
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
	for _, member := range members {
		resp, err := v.client.Post(member.BatchPosterAddress, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("http request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Error("server returned returned error", "error code", resp.StatusCode)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Error("failed to read response body", "err", err, "url", member.BatchPosterAddress)
			continue
		}
		var rpcResponse BatchRpcResponse
		if err := json.Unmarshal(body, &rpcResponse); err != nil {
			log.Error("failed to unmarshal json response", "err", err, "url", member.BatchPosterAddress)
			continue
		}
		if rpcResponse.Error != nil {
			log.Error("got error from request", "err", rpcResponse.Error.Message, "url", member.BatchPosterAddress)
			continue
		}

		pubKey, err := crypto.SigToPub(args.Hash, rpcResponse.Result)
		if err != nil {
			log.Error("failed to recover public key", "err", err, "url", member.BatchPosterAddress)
			continue
		}

		if !bytes.Equal(crypto.CompressPubkey(pubKey), member.SigKey) {
			log.Error("failed to validate signature in the committee", "url", member.BatchPosterAddress)
			continue
		}
		sigLength := len(rpcResponse.Result)
		if sigLength > 0 {
			// Get the last byte (v)
			vIndex := sigLength - 1
			v := rpcResponse.Result[vIndex]

			// Adjusting ECDSA signature 'v' value for Ethereum compatibility
			// Get `v` from the signature and verify the byte is in expected format for openzeppelin `ECDSA.recover`
			// https://github.com/ethereum/go-ethereum/issues/19751
			if v == 0 || v == 1 {
				rpcResponse.Result[vIndex] = v + 27
			}
		}
		sigs = append(sigs, rpcResponse.Result)
	}
	if len(sigs) < requiredQuorum {
		return nil, fmt.Errorf("did not receive enough valid signatures for batch correctness. wanted: %d, have: %d", requiredQuorum, len(sigs))
	}
	return bytes.Join(sigs, nil), nil
}

func (v *BatchVerifier) HashBatchData(data []byte) []byte {
	return crypto.Keccak256Hash(data).Bytes()
}

func (v *BatchVerifier) HashAndSignBatchData(
	signingData []byte,
) (*BatchPosterArgs, error) {
	hash := v.HashBatchData(signingData)
	signature, err := crypto.Sign(hash, v.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign data: %w", err)
	}
	args := &BatchPosterArgs{
		SignedData: signingData,
		Signature:  signature,
		Hash:       hash,
		PubKey:     crypto.CompressPubkey(&v.privateKey.PublicKey),
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
	timeboostKeyManager *decentralizedtimeboostgen.KeyManager,
	arguments abi.Arguments,
	seqNum *big.Int,
	l2MessageData []byte,
	delayedMsg *big.Int,
	gasRefunder common.Address,
	prevMsgNum *big.Int,
	newMsgNum *big.Int,
) ([]byte, error) {
	committee, err := timeboostKeyManager.GetCommitteeById(&bind.CallOpts{}, 0)
	if err != nil {
		return nil, err
	}
	leader := committee.Members[seqNum.Uint64()%uint64(len(committee.Members))]
	pubKey := v.getCompressedPubKey()
	if !bytes.Equal(pubKey, leader.SigKey) {
		log.Debug(
			"batch not sent: not leader",
			"key", hex.EncodeToString(pubKey),
			"sequenceNumber", seqNum,
			"from", prevMsgNum,
			"to", newMsgNum,
			"prevDelayed", delayedMsg,
		)
		return nil, fmt.Errorf("not leader for batch")
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
	timeboostKeyManager *decentralizedtimeboostgen.KeyManager,
	seqNum *big.Int,
	l2MessageData []byte,
	delayedMsg *big.Int,
	gasRefunder common.Address,
	prevMsgNum *big.Int,
	newMsgNum *big.Int,
	encodedBlobs []byte,
) ([]byte, error) {
	committee, err := timeboostKeyManager.GetCommitteeById(&bind.CallOpts{}, 0)
	if err != nil {
		return nil, err
	}
	leader := committee.Members[seqNum.Uint64()%uint64(len(committee.Members))]
	pubKey := v.getCompressedPubKey()
	if !bytes.Equal(pubKey, leader.SigKey) {
		log.Debug(
			"batch not sent: not leader",
			"key", hex.EncodeToString(pubKey),
			"sequenceNumber", seqNum,
			"from", prevMsgNum,
			"to", *newMsgNum,
			"prevDelayed", delayedMsg,
		)
		return nil, fmt.Errorf("not leader for batch")
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
