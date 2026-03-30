package espressotee

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbnode/dataposter"
)

type TEE uint8

const (
	SGX   TEE = 0 // SGX
	NITRO TEE = 1 // AWS Nitro

	EMPTY TEE = 254
	TESTS TEE = 2
	// Define the empty string, which coudld be useful in certain circumstances
	// Also define empty and test related contents at the end of the types range to make room
	// for other sequential TEE types.
)

const (
	EspressoMaxTxnWaitTime                = 3 * time.Minute
	EspressoRetryReadContractDelay        = 5 * time.Second
	EspressoMaxRetries                    = 5
	EspressoGasLimitBufferIncreasePercent = 20
)

func FromString(s string) (TEE, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "SGX":
		return SGX, nil
	case "NITRO":
		return NITRO, nil
	case "TESTS":
		return TESTS, nil
	case "":
		return EMPTY, nil
	default:
		return 0, fmt.Errorf("invalid TEE type: %q", s)
	}
}

type ContractVerificationFunc func() (bool, error)

func ContractVerification(
	fn ContractVerificationFunc,
	msg string,
) (bool, error) {
	var err error
	success := false
	for attempt := 0; attempt < EspressoMaxRetries; attempt++ {
		success, err = fn()
		if err != nil {
			log.Error(msg, "err", err)
		}
		if success {
			return true, nil
		}

		if attempt < EspressoMaxRetries-1 {
			log.Error(msg, "attempt", attempt, "retry delay", EspressoRetryReadContractDelay)
			time.Sleep(EspressoRetryReadContractDelay)
		}
	}
	return false, nil
}

/**
 * This functions checks the dataposter nonce and the parent chains nonce
 * If these two differ, dont send a transaction as registering the signer is costly and we dont want to send multiple transactions.
 * This will constantly be called when we try and post a batch which will allow time for the two to eventually sync up.
 */
func NonceValidation(context context.Context, l1Client *ethclient.Client, dataPoster *dataposter.DataPoster) error {
	nonce, err := l1Client.NonceAt(context, dataPoster.Sender(), nil)
	if err != nil {
		log.Warn("could not retrieve on-chain nonce", "err", err)
		return err
	}
	dataPosterNonce, _, err := dataPoster.GetNextNonceAndMeta(context)
	if err != nil {
		log.Warn("error getting dataposter nonce", "err", err)
		return err
	}
	log.Info("successfully got datapaster next nonce and on-chain nonce", "dataposter nonce", dataPosterNonce, "on-chain nonce", nonce)
	if dataPosterNonce != nonce {
		return fmt.Errorf("dataposter nonce %d does not match on-chain nonce %d", dataPosterNonce, nonce)
	}
	return nil
}
