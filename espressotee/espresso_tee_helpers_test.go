package espressotee

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/offchainlabs/nitro/arbnode/dataposter"
	"github.com/offchainlabs/nitro/util/headerreader"
)

type nonceStub struct {
	nonce uint64
}

func (s *nonceStub) CallContext(_ context.Context, result interface{}, method string, _ ...interface{}) error {
	switch method {
	case "eth_getTransactionCount":
		ptr, ok := result.(*hexutil.Uint64)
		if !ok {
			return errors.New("result is not a *hexutil.Uint64")
		}
		*ptr = hexutil.Uint64(s.nonce)
	case "eth_getBlockByNumber":

		ptr, ok := result.(**types.Header)
		if !ok {
			return errors.New("result is not a **types.Header")
		}
		*ptr = &types.Header{Number: big.NewInt(1)}
	}
	return nil
}

func (s *nonceStub) EthSubscribe(_ context.Context, _ interface{}, _ ...interface{}) (*rpc.ClientSubscription, error) {
	return nil, nil
}

func (s *nonceStub) BatchCallContext(_ context.Context, _ []rpc.BatchElem) error {
	return nil
}

func (s *nonceStub) Close() {}

func buildDataPoster(t *testing.T, ctx context.Context, dataPosterNonce uint64, senderAddr common.Address) *dataposter.DataPoster {
	t.Helper()

	dpClient := ethclient.NewClient(&nonceStub{nonce: dataPosterNonce})

	hr, err := headerreader.New(ctx, dpClient, func() *headerreader.Config {
		cfg := headerreader.DefaultConfig
		return &cfg
	}, nil)
	if err != nil {
		t.Fatalf("failed to create header reader: %v", err)
	}

	testCfg := dataposter.TestDataPosterConfig
	dp, err := dataposter.NewDataPoster(ctx, &dataposter.DataPosterOpts{
		HeaderReader: hr,
		Auth:         &bind.TransactOpts{From: senderAddr},
		Config:       func() *dataposter.DataPosterConfig { return &testCfg },
		MetadataRetriever: func(_ context.Context, _ *big.Int) ([]byte, error) {
			return nil, nil
		},
		ParentChainID: big.NewInt(1337),
	})
	if err != nil {
		t.Fatalf("failed to create data poster: %v", err)
	}
	return dp
}

// DataPoster reads nonce 5 from its stub; l1Client reports 6 — mismatch.
func TestEspressoNonceValidationMismatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sender := common.HexToAddress("0xdeadbeef")

	dp := buildDataPoster(t, ctx, 5, sender)
	l1Client := ethclient.NewClient(&nonceStub{nonce: 6})

	err := NonceValidation(ctx, l1Client, dp)
	if err == nil {
		t.Fatal("expected error for nonce mismatch, got nil")
	}
	if !errors.Is(err, ErrNonceValidation) {
		t.Fatalf("expected ErrNonceValidation, got: %v", err)
	}
}

// Both sides agree on nonce 5 — should pass.
func TestEspressoNonceValidationMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sender := common.HexToAddress("0xdeadbeef")

	dp := buildDataPoster(t, ctx, 5, sender)
	l1Client := ethclient.NewClient(&nonceStub{nonce: 5})

	err := NonceValidation(ctx, l1Client, dp)
	if err != nil {
		t.Fatalf("expected no error for matching nonces, got: %v", err)
	}
}
