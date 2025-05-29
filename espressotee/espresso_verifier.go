package espressotee

import (
	"context"
	"errors"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbnode/dataposter"
	"github.com/offchainlabs/nitro/solgen/go/espressogen"
)

type EspressoTEEVerifierInterface interface {
	RegisterSigner(opts *bind.TransactOpts, to common.Address, poster *dataposter.DataPoster, attestation []byte, data []byte, teeType uint8) error
	RegisteredSigners(signer common.Address, teeType uint8) (bool, error)
}

type EspressoTEEVerifier struct {
	contract *espressogen.IEspressoTEEVerifier
	l1Client *ethclient.Client
}

func NewEspressoTEEVerifier(contract *espressogen.IEspressoTEEVerifier, l1Client *ethclient.Client) *EspressoTEEVerifier {
	return &EspressoTEEVerifier{contract: contract, l1Client: l1Client}
}

func (e *EspressoTEEVerifier) RegisterSigner(opts *bind.TransactOpts, to common.Address, poster *dataposter.DataPoster, attestation []byte, data []byte, teeType uint8) error {
	contractABI, err := abi.JSON(strings.NewReader(espressogen.IEspressoTEEVerifierMetaData.ABI))
	if err != nil {
		return err
	}

	// Pack the function arguments (attestation, data, teeType)
	calldata, err := contractABI.Pack("registerSigner", attestation, data, teeType)
	if err != nil {
		return err
	}
	msg := ethereum.CallMsg{
		From:  opts.From,
		To:    &to,
		Data:  calldata,
		Value: opts.Value,
	}

	estimate, err := e.l1Client.EstimateGas(context.Background(), msg)
	if err != nil {
		return err
	}
	log.Info("estimate", "e", estimate)
	higher := estimate + 9000000
	tx, err := poster.PostSimpleTransaction(context.Background(), to, calldata, higher, opts.Value)
	if err != nil {
		return err
	}

	log.Info("Waiting for register signer tx to be mined", "tx", tx.Hash())

	receipt, err := bind.WaitMined(context.Background(), e.l1Client, tx)
	if err != nil {
		return err
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return errors.New("transaction failed")
	}

	log.Info("Register signer tx succeeded", "tx", tx.Hash().Hex())

	return nil
}

func (e *EspressoTEEVerifier) RegisteredSigners(address common.Address, teeType uint8) (bool, error) {
	return e.contract.RegisteredSigners(&bind.CallOpts{}, address, teeType)
}
