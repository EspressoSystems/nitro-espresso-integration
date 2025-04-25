package arbnode

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/espressotee"
	"github.com/offchainlabs/nitro/solgen/go/espressogen"
	"github.com/offchainlabs/nitro/util/signature"
)

func recoverAddressFromSigner(signer signature.DataSignerFunc) (common.Address, error) {
	message := make([]byte, 32)
	signature, err := signer(message)
	if err != nil {
		return common.Address{}, err
	}

	publicKey, err := crypto.SigToPub(message, signature)
	if err != nil {
		return common.Address{}, err
	}

	return crypto.PubkeyToAddress(*publicKey), nil
}

func setupNitroVerifier(teeVerifier *espressogen.IEspressoTEEVerifier, l1Client *ethclient.Client) (espressotee.EspressoNitroTEEVerifierInterface, error) {
	// Setup nitro contract interface
	nitroAddr, err := teeVerifier.EspressoNitroTEEVerifier(&bind.CallOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to get nitro tee verifier address from caller: %v", err)
	}
	log.Info("succesfully retrieved nitro contract verifier address", "address", nitroAddr)

	nitroVerifierBindings, err := espressogen.NewIEspressoNitroTEEVerifier(
		nitroAddr,
		l1Client)
	if err != nil {
		return nil, err
	}
	nitroVerifier := espressotee.NewEspressoNitroTEEVerifier(nitroVerifierBindings, l1Client)
	return nitroVerifier, nil
}
