package espressotee

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/arbnode/dataposter"
	legacy_espressogen "github.com/offchainlabs/nitro/espresso-tee-contracts-legacy/espressogen"
	"github.com/offchainlabs/nitro/espresso-tee-contracts/espressogen"
)

type EspressoTEEVerifierInterface interface {
	RegisterService(
		dataPoster *dataposter.DataPoster,
		attestation []byte,
		data []byte,
		teeType uint8,
		serviceType ServiceType,
		registerSignerOpts EspressoRegisterServiceOpts,
	) error
	RegisteredServices(
		signer common.Address,
		teeType uint8,
		serviceType ServiceType,
		registerSignerOpts EspressoRegisterServiceOpts,
	) (bool, error)
}

type EspressoTEEVerifier struct {
	espressoTEEVerifierAddress string
	l1Client                   *ethclient.Client
	address                    common.Address
}

func NewEspressoTEEVerifier(espressoTEEVerifierAddress string, l1Client *ethclient.Client, address common.Address) *EspressoTEEVerifier {

	return &EspressoTEEVerifier{espressoTEEVerifierAddress: espressoTEEVerifierAddress, l1Client: l1Client, address: address}
}

func (e *EspressoTEEVerifier) RegisterService(
	dataPoster *dataposter.DataPoster,
	attestation []byte,
	data []byte,
	teeType uint8,
	serviceType ServiceType,
	registerSignerOpts EspressoRegisterServiceOpts,
) error {
	switch serviceType {
	case CaffNode:
		return e.registerService(dataPoster, attestation, data, teeType, serviceType, registerSignerOpts)
	case BatchPoster:
		return e.registerSigner(dataPoster, attestation, data, teeType, registerSignerOpts)
	}

	return fmt.Errorf("unsupported service type: %d", serviceType)
}

func (e *EspressoTEEVerifier) registerService(
	dataPoster *dataposter.DataPoster,
	attestation []byte,
	data []byte,
	teeType uint8,
	serviceType ServiceType,
	registerSignerOpts EspressoRegisterServiceOpts,
) error {
	// First check base fee is low enough
	err := BaseFeeCheck(
		registerSignerOpts.MaxBaseFee,
		registerSignerOpts.MaxRetries,
		registerSignerOpts.RetryBaseFeeDelay,
		func() (*big.Int, error) {
			return dataPoster.BaseFee()
		},
		"register signer: latest base fee is greater than max base fee",
	)
	if err != nil {
		return err
	}

	contractABI, err := espressogen.IEspressoTEEVerifierMetaData.GetAbi()
	if err != nil {
		return err
	}

	// Pack the function arguments (attestation, data, teeType, serviceType)
	calldata, err := contractABI.Pack("registerService", attestation, data, teeType, serviceType)
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

	err = NonceValidation(context.Background(), e.l1Client, dataPoster)
	if err != nil {
		return err
	}
	// Add a buffer to the estimate for the gas limit
	gasLimit := estimate * (100 + registerSignerOpts.GasLimitBufferIncreasePercent) / 100
	log.Info("register signer gas limit", "gas limit", gasLimit)

	// Since we use batch poster private key to register signer, we need to use dataposter to post transaction
	// So the dataposter can track the proper nonce once we start posting batches
	tx, err := dataPoster.PostSimpleTransaction(context.Background(), e.address, calldata, gasLimit, dataPoster.Auth().Value)
	if err != nil {
		log.Info("failed to post register signer transaction", "err", err)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), registerSignerOpts.MaxTxnWaitTime)
	defer cancel()
	log.Info("waiting for register signer tx to be mined", "tx", tx.Hash().Hex(), "timeout", registerSignerOpts.MaxTxnWaitTime)

	receipt, err := bind.WaitMined(ctx, e.l1Client, tx)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf(
				"register signer timed out after %v minutes waiting for tx %s to be mined",
				registerSignerOpts.MaxTxnWaitTime,
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
	teeType uint8,
	serviceType ServiceType,
	registerSignerOpts EspressoRegisterServiceOpts,
) (bool, error) {
	switch serviceType {
	case CaffNode:
		return e.registeredServices(signer, teeType, serviceType, registerSignerOpts)
	case BatchPoster:
		return e.registeredSigners(signer, teeType, registerSignerOpts)
	}

	return false, fmt.Errorf("unsupported service type: %d", serviceType)
}

func (e *EspressoTEEVerifier) registeredServices(address common.Address, teeType uint8, serviceType ServiceType, registerSignerOpts EspressoRegisterServiceOpts) (bool, error) {
	contract, err := espressogen.NewIEspressoTEEVerifier(e.address, e.l1Client)
	if err != nil {
		return false, err
	}
	ok, err := ContractVerification(
		registerSignerOpts.MaxRetries,
		registerSignerOpts.RetryReadContractDelay,
		func() (bool, error) {
			return contract.RegisteredServices(&bind.CallOpts{}, address, teeType, uint8(serviceType))
		},
		"register services - address not yet registered in contract",
	)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (e *EspressoTEEVerifier) registerSigner(
	dataPoster *dataposter.DataPoster,
	attestation []byte,
	data []byte,
	teeType uint8,
	registerSignerOpts EspressoRegisterServiceOpts,
) error {
	// First check base fee is low enough
	err := BaseFeeCheck(
		registerSignerOpts.MaxBaseFee,
		registerSignerOpts.MaxRetries,
		registerSignerOpts.RetryBaseFeeDelay,
		func() (*big.Int, error) {
			return dataPoster.BaseFee()
		},
		"register signer: latest base fee is greater than max base fee",
	)
	if err != nil {
		return err
	}

	contractABI, err := legacy_espressogen.IEspressoTEEVerifierMetaData.GetAbi()
	if err != nil {
		return err
	}

	// Pack the function arguments (attestation, data, teeType)
	calldata, err := contractABI.Pack("registerSigner", attestation, data, teeType)
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

	err = NonceValidation(context.Background(), e.l1Client, dataPoster)
	if err != nil {
		return err
	}
	// Add a buffer to the estimate for the gas limit
	gasLimit := estimate * (100 + registerSignerOpts.GasLimitBufferIncreasePercent) / 100
	log.Info("register signer gas limit", "gas limit", gasLimit)

	// Since we use batch poster private key to register signer, we need to use dataposter to post transaction
	// So the dataposter can track the proper nonce once we start posting batches
	tx, err := dataPoster.PostSimpleTransaction(context.Background(), e.address, calldata, gasLimit, dataPoster.Auth().Value)
	if err != nil {
		log.Info("failed to post register signer transaction", "err", err)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), registerSignerOpts.MaxTxnWaitTime)
	defer cancel()
	log.Info("waiting for register signer tx to be mined", "tx", tx.Hash().Hex(), "timeout", registerSignerOpts.MaxTxnWaitTime)

	receipt, err := bind.WaitMined(ctx, e.l1Client, tx)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf(
				"register signer timed out after %v minutes waiting for tx %s to be mined",
				registerSignerOpts.MaxTxnWaitTime,
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

func (e *EspressoTEEVerifier) registeredSigners(address common.Address, teeType uint8, registerSignerOpts EspressoRegisterServiceOpts) (bool, error) {
	contract, err := legacy_espressogen.NewIEspressoTEEVerifier(e.address, e.l1Client)
	if err != nil {
		return false, err
	}
	ok, err := ContractVerification(
		registerSignerOpts.MaxRetries,
		registerSignerOpts.RetryReadContractDelay,
		func() (bool, error) {
			return contract.RegisteredSigners(&bind.CallOpts{}, address, teeType)
		},
		"register signers - address not yet registered in contract",
	)
	if err != nil {
		return false, err
	}
	return ok, nil
}
