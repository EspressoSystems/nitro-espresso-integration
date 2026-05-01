package espressotee

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbnode/dataposter"
	"github.com/offchainlabs/nitro/espresso-tee-contracts/espressogen"
)

type EspressoTEEVerifierInterface interface {
	RegisterService(
		dataPoster *dataposter.DataPoster,
		attestation []byte,
		data []byte,
		teeType uint8,
	) error
	RegisteredServices(
		signer common.Address,
		teeType TEE,
	) (bool, error)
	EspressoTEEAddress() common.Address
	ParentChainId() (uint64, error)
	CheckNonceValidation(dataPoster *dataposter.DataPoster) error
}

type EspressoTEEVerifier struct {
	espressoTEEVerifierAddress string
	l1Client                   *ethclient.Client
	address                    common.Address
}

var _ EspressoTEEVerifierInterface = (*EspressoTEEVerifier)(nil)

func NewEspressoTEEVerifier(espressoTEEVerifierAddress string, l1Client *ethclient.Client, address common.Address) *EspressoTEEVerifier {

	return &EspressoTEEVerifier{espressoTEEVerifierAddress: espressoTEEVerifierAddress, l1Client: l1Client, address: address}
}
func (e *EspressoTEEVerifier) RegisterService(
	dataPoster *dataposter.DataPoster,
	attestation []byte,
	data []byte,
	teeType uint8,
) error {
	var registrationErr error

	for attempt := 0; attempt < EspressoMaxRetries; attempt++ {
		registrationErr = e.registerService(
			dataPoster,
			attestation,
			data,
			teeType,
		)

		if registrationErr == nil {
			return nil
		}

		log.Warn(
			"service registration failed",
			"err", registrationErr,
			"attempt", attempt+1,
		)
		if attempt < EspressoMaxRetries-1 {
			time.Sleep(EspressoRetryReadContractDelay)
		}
	}

	return fmt.Errorf(
		"service registration failed after %d attempts: %w",
		EspressoMaxRetries,
		registrationErr,
	)
}

func (e *EspressoTEEVerifier) CheckNonceValidation(dataPoster *dataposter.DataPoster) error {
	err := NonceValidation(context.Background(), e.l1Client, dataPoster)
	if err != nil {
		return fmt.Errorf("nonce validation failed: %w", err)
	}
	return nil
}

func (e *EspressoTEEVerifier) registerService(
	dataPoster *dataposter.DataPoster,
	attestation []byte,
	data []byte,
	teeType uint8,
) error {
	contractABI, err := espressogen.IEspressoTEEVerifierMetaData.GetAbi()
	if err != nil {
		return err
	}

	// Pack the function arguments (attestation, data, teeType)
	calldata, err := contractABI.Pack("registerService", attestation, data, teeType)
	if err != nil {
		return err
	}
	msg := ethereum.CallMsg{
		From:  dataPoster.Sender(),
		To:    &e.address,
		Data:  calldata,
		Value: dataPoster.Auth().Value,
	}

	estimate, err := e.l1Client.EstimateGas(context.Background(), msg)
	if err != nil {
		return err
	}

	// Add a buffer to the estimate for the gas limit
	gasLimit := estimate * (100 + EspressoGasLimitBufferIncreasePercent) / 100
	log.Info("register signer gas limit", "gas limit", gasLimit)

	// Since we use batch poster private key to register signer, we need to use dataposter to post transaction
	// So the dataposter can track the proper nonce once we start posting batches
	tx, err := dataPoster.PostSimpleTransaction(context.Background(), e.address, calldata, gasLimit, dataPoster.Auth().Value)
	if err != nil {
		log.Info("failed to post register signer transaction", "err", err)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), EspressoMaxTxnWaitTime)
	defer cancel()
	log.Info("waiting for register signer tx to be mined", "tx", tx.Hash().Hex(), "timeout", EspressoMaxTxnWaitTime)

	receipt, err := bind.WaitMined(ctx, e.l1Client, tx)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf(
				"register signer timed out after %v minutes waiting for tx %s to be mined",
				EspressoMaxTxnWaitTime,
				tx.Hash().Hex(),
			)
		}
		return err
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return errors.New("transaction failed")
	}

	log.Info("register signer tx succeeded", "tx", tx.Hash().Hex())

	return nil
}

func (e *EspressoTEEVerifier) RegisteredServices(
	signer common.Address,
	teeType TEE,
) (bool, error) {
	return e.registeredServices(signer, teeType)
}

func (e *EspressoTEEVerifier) EspressoTEEAddress() common.Address {
	return e.address
}

func (e *EspressoTEEVerifier) ParentChainId() (uint64, error) {
	chainID, err := e.l1Client.ChainID(context.Background())
	if err != nil {
		return 0, err
	}
	return chainID.Uint64(), nil
}

func (e *EspressoTEEVerifier) registeredServices(address common.Address, teeType TEE) (bool, error) {
	contract, err := espressogen.NewIEspressoTEEVerifier(e.address, e.l1Client)
	if err != nil {
		return false, err
	}
	ok, err := ContractVerification(
		func() (bool, error) {
			return contract.IsSignerValid(&bind.CallOpts{}, address, uint8(teeType))
		},
		"register services - address not yet registered in contract",
	)
	if err != nil {
		return false, err
	}
	return ok, nil
}
