package espressotee

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// registrationAttemptStub drives TestEspressoRegistrationNonceErrorsNotCounted.
//
// For the first mismatchFor calls to eth_estimateGas it returns success while
// responding to eth_getTransactionCount with a nonce that never matches the
// DataPoster's nonce, so NonceValidation returns ErrNonceValidation.
// Once mismatchFor is exceeded, eth_estimateGas starts returning an error,
// producing a non-nonce failure that does count as a registration attempt.
type registrationAttemptStub struct {
	estimateGasCalls int
	mismatchFor      int
	l1Nonce          uint64 // always returned for eth_getTransactionCount
}

func (s *registrationAttemptStub) CallContext(_ context.Context, result interface{}, method string, _ ...interface{}) error {
	switch method {
	case "eth_estimateGas":
		s.estimateGasCalls++
		if s.estimateGasCalls > s.mismatchFor {
			return errors.New("simulated gas estimation failure")
		}
		if ptr, ok := result.(*hexutil.Uint64); ok {
			*ptr = 0
		}
	case "eth_getTransactionCount":
		if ptr, ok := result.(*hexutil.Uint64); ok {
			*ptr = hexutil.Uint64(s.l1Nonce)
		}
	case "eth_getBlockByNumber":
		if ptr, ok := result.(**types.Header); ok {
			*ptr = &types.Header{Number: big.NewInt(1)}
		}
	}
	return nil
}

func (s *registrationAttemptStub) EthSubscribe(_ context.Context, _ interface{}, _ ...interface{}) (*rpc.ClientSubscription, error) {
	return nil, nil
}

func (s *registrationAttemptStub) BatchCallContext(_ context.Context, _ []rpc.BatchElem) error {
	return nil
}

func (s *registrationAttemptStub) Close() {}

// TestEspressoRegistrationNonceErrorsNotCounted verifies that ErrNonceValidation
// errors do not consume registration attempt slots.
func TestEspressoRegistrationNonceErrorsNotCounted(t *testing.T) {
	t.Parallel()

	const (
		dataPosterNonce = uint64(5)
		l1MismatchNonce = uint64(6) // never matches dataPosterNonce
		mismatchFor     = 1         // one nonce error before EstimateGas starts failing
	)

	ctx := context.Background()
	sender := common.HexToAddress("0xdeadbeef")

	stub := &registrationAttemptStub{
		mismatchFor: mismatchFor,
		l1Nonce:     l1MismatchNonce,
	}

	dp := buildDataPoster(t, ctx, dataPosterNonce, sender)
	verifier := &EspressoTEEVerifier{
		l1Client: ethclient.NewClient(stub),
		address:  common.HexToAddress("0x1234"),
	}

	err := verifier.RegisterService(dp, []byte("attestation"), []byte("data"), uint8(SGX), Test)
	if err == nil {
		t.Fatal("expected RegisterService to fail, got nil")
	}

	// Nonce errors don't count → loop runs a full EspressoMaxRetries real failures
	// on top of the mismatchFor nonce-only calls.
	expected := mismatchFor + EspressoMaxRetries
	if stub.estimateGasCalls != expected {
		t.Fatalf(
			"expected %d EstimateGas calls (%d nonce errors + %d real failures), got %d — "+
				"nonce errors may be incorrectly counted as registration attempts",
			expected, mismatchFor, EspressoMaxRetries, stub.estimateGasCalls,
		)
	}
}
