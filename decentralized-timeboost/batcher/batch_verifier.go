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

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbutil"
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

func NewBatchVerifier(privateKey *ecdsa.PrivateKey) (*BatchVerifier, error) {
	if privateKey == nil {
		return nil, fmt.Errorf("private key cannot be nil")
	}
	return &BatchVerifier{
		privateKey: privateKey,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				Dial: func(network, addr string) (net.Conn, error) {
					return net.Dial(network, addr)
				},
			},
		},
	}, nil
}

func (v *BatchVerifier) GetCompressedPubKey() []byte {
	return crypto.CompressPubkey(&v.privateKey.PublicKey)
}

func (v *BatchVerifier) SendBatchForVerification(
	args *BatchPosterArgs,
	sigKeyMap map[string]bool,
) ([][]byte, error) {
	requiredQuorum := 2*(len(sigKeyMap)-1)/3 + 1
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

	// TODO: Read from contract, currently not in the contract
	urls := []string{"http://localhost:8945", "http://localhost:8947"}
	var sigs [][]byte
	for _, url := range urls {
		resp, err := v.client.Post(url, "application/json", bytes.NewBuffer(jsonData))
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
			log.Error("failed to read response body", "err", err, "url", url)
			continue
		}
		var rpcResponse BatchRpcResponse
		if err := json.Unmarshal(body, &rpcResponse); err != nil {
			log.Error("failed to unmarshal json response", "err", err, "url", url)
			continue
		}
		if rpcResponse.Error != nil {
			log.Error("got error from request", "err", rpcResponse.Error.Message, "url", url)
			continue
		}

		pubKey, err := crypto.SigToPub(args.Hash, rpcResponse.Result)
		if err != nil {
			log.Error("failed to recover public key", "err", err, "url", url)
			continue
		}

		if !sigKeyMap[string(crypto.CompressPubkey(pubKey))] {
			log.Error("failed to validate signature in the committee", "url", url)
			continue
		}
		sigs = append(sigs, rpcResponse.Result)
	}
	if len(sigs) < requiredQuorum {
		return nil, fmt.Errorf("did not receive enough valid signatures for batch correctness. wanted: %d, have: %d", requiredQuorum, len(sigs))
	}
	return sigs, nil
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

func (v *BatchVerifier) getAbiArguments() (*abi.Arguments, error) {
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
	return &abi.Arguments{
		{Name: "sequencerNumber", Type: uint256Type},
		{Name: "data", Type: bytesType},
		{Name: "afterDelayedMessagesRead", Type: uint256Type},
		{Name: "gasRefunder", Type: addressType},
		{Name: "prevMessageCount", Type: uint256Type},
		{Name: "newMessageCount", Type: uint256Type},
	}, nil
}

func (v *BatchVerifier) GetSignedDataFromBytes(data []byte) (*SigningData, error) {
	arguments, err := v.getAbiArguments()
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

	arguments, err := v.getAbiArguments()
	if err != nil {
		return err
	}

	calldata, err := arguments.Pack(
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

	hash := v.HashBatchData(calldata)
	if !bytes.Equal(hash, args.Hash) {
		return fmt.Errorf("failed to verify hash, calculated hash. calculated: 0x%s, received: 0x:%s", hex.EncodeToString(hash), hex.EncodeToString(args.Hash))
	}

	if err := v.VerifySignatureOverHash(args.Hash, args.Signature, args.PubKey); err != nil {
		return fmt.Errorf("failed to verify signature: %w", err)
	}
	return nil
}
