package espressotee

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/offchainlabs/nitro/solgen/go/espressogen"
	"github.com/ethereum/go-ethereum/log"
)

type EspressoTEEVerifierInterface interface {
	RegisterSigner(opts *bind.TransactOpts, attestation []byte, addr []byte) error
	RegisteredSigners(signer common.Address) (bool, error)
	Verify(opts *bind.CallOpts, signature []byte, userDataHash [32]byte) (bool, error)
}

type EspressoTEEVerifier struct {
	teeType  uint8
	l1Client *ethclient.Client
	verifier *espressogen.IEspressoTEEVerifier
}

func NewEspressoTEEVerifier(l1Client *ethclient.Client, addr common.Address, teeType uint8) (*EspressoTEEVerifier, error) {
	verifier, err := espressogen.NewIEspressoTEEVerifier(addr, l1Client)
	if err != nil {
		return nil, err
	}
	return &EspressoTEEVerifier{
		teeType:  teeType,
		l1Client: l1Client,
		verifier: verifier,
	}, nil
}
// Verify:
//        This function calls verify on the bound EspressoTEEVerifier contract
// Parameters:
//        opts: default call opts.
//        signature: batchers signature with ephemeral key over the batch data.
//        userDataHash: the hash of the user data that was signed.
// Returns:
//        True if the verification succeeds, false, if it does not, or false and an error if an error occurs.
func (e *EspressoTEEVerifier) Verify(opts *bind.CallOpts, signature []byte, userDataHash [32]byte) (bool, error) {
  res, err := e.verifier.Verify(opts, signature, userDataHash, e.teeType)
  if err != nil{
    return false, err
  }
  return res, nil
}
// RegisterSigner:
//        This function calls RegisterSigner on the bound contract.
// Parameters:
//        opts: Transaction details
//        attestation: The tee attestation to be verified to register the signer.
//        addr: The address associated with the ephemeral key to be registered.
// Returns:
//        An error if one occurs. The returned value being nil indicates that the transaction to register the signer was
//        has succeeded and been mined on chain.
func (e *EspressoTEEVerifier) RegisterSigner(opts *bind.TransactOpts, attestation []byte, addr []byte) error {
	tx, err := e.verifier.RegisterSigner(opts, attestation, addr, e.teeType)
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

	log.Info("Register signer tx succeeded", "signer address", addr, "tx", tx.Hash())

	return nil
}

// RegisteredSigners:
//        This function calls RegisteredSigners on the bound contract.
// Parameters:
//        Address:
//        The address to query the registration status of.
// Returns:
//        boolean:
//          Representing if parameter address is a registered signer.
//          This will be false if an error occurrs.
func (e *EspressoTEEVerifier) RegisteredSigners(address common.Address) (bool, error) {
	return e.verifier.RegisteredSigners(&bind.CallOpts{}, address, e.teeType)
}
