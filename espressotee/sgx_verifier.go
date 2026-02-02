package espressotee

import (
	"context"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type EspressoSGXVerifierInterface interface {
	Verify(opts *bind.CallOpts, rawQuote []byte, reportDataHash [32]byte) (EnclaveReport, error)
}

type EspressoSGXVerifier struct {
	l1Client *ethclient.Client
	address  common.Address
}

func (v *EspressoSGXVerifier) Verify(opts *bind.CallOpts, rawQuote []byte, reportDataHash [32]byte) (EnclaveReport, error) {
	// Minimal ABI for: verify(bytes rawQuote, bytes32 reportDataHash) -> (EnclaveReport)
	// We only need this to pack calldata + unpack the return value.
	parsedABI, err := abi.JSON(strings.NewReader(`[
		{"type":"function","name":"verify","stateMutability":"view",
		 "inputs":[
			{"name":"rawQuote","type":"bytes"},
			{"name":"reportDataHash","type":"bytes32"}
		 ],
		 "outputs":[{"name":"","type":"tuple","components":[
			{"name":"cpuSvn","type":"bytes16"},
			{"name":"miscSelect","type":"bytes4"},
			{"name":"reserved1","type":"bytes28"},
			{"name":"attributes","type":"bytes16"},
			{"name":"mrEnclave","type":"bytes32"},
			{"name":"reserved2","type":"bytes32"},
			{"name":"mrSigner","type":"bytes32"},
			{"name":"reserved3","type":"bytes"},
			{"name":"isvProdId","type":"uint16"},
			{"name":"isvSvn","type":"uint16"},
			{"name":"reserved4","type":"bytes"},
			{"name":"reportData","type":"bytes"}
		 ]}]
		}
	]`))
	if err != nil {
		return EnclaveReport{}, err
	}

	method := parsedABI.Methods["verify"]
	args, err := method.Inputs.Pack(rawQuote, reportDataHash)
	if err != nil {
		return EnclaveReport{}, err
	}
	calldata := append([]byte{}, method.ID...)
	calldata = append(calldata, args...)

	ctx := context.Background()
	var blockNumber *big.Int
	msg := ethereum.CallMsg{To: &v.address, Data: calldata}
	if opts != nil {
		if opts.Context != nil {
			ctx = opts.Context
		}
		msg.From = opts.From
		blockNumber = opts.BlockNumber
	}

	ret, err := v.l1Client.CallContract(ctx, msg, blockNumber)
	if err != nil {
		return EnclaveReport{}, err
	}

	// Use an internal type to ensure ABI tuple field names line up during unpacking.
	type enclaveReportABI struct {
		CpuSvn     [16]byte `abi:"cpuSvn"`
		MiscSelect [4]byte  `abi:"miscSelect"`
		Reserved1  [28]byte `abi:"reserved1"`
		Attributes [16]byte `abi:"attributes"`
		MrEnclave  [32]byte `abi:"mrEnclave"`
		Reserved2  [32]byte `abi:"reserved2"`
		MrSigner   [32]byte `abi:"mrSigner"`
		Reserved3  []byte   `abi:"reserved3"`
		IsvProdId  uint16   `abi:"isvProdId"`
		IsvSvn     uint16   `abi:"isvSvn"`
		Reserved4  []byte   `abi:"reserved4"`
		ReportData []byte   `abi:"reportData"`
	}

	var decoded enclaveReportABI
	if err := parsedABI.UnpackIntoInterface(&decoded, "verify", ret); err != nil {
		return EnclaveReport{}, err
	}

	return EnclaveReport{
		CpuSvn:     decoded.CpuSvn,
		MiscSelect: decoded.MiscSelect,
		Reserved1:  decoded.Reserved1,
		Attributes: decoded.Attributes,
		MrEnclave:  decoded.MrEnclave,
		Reserved2:  decoded.Reserved2,
		MrSigner:   decoded.MrSigner,
		Reserved3:  decoded.Reserved3,
		IsvProdId:  decoded.IsvProdId,
		IsvSvn:     decoded.IsvSvn,
		Reserved4:  decoded.Reserved4,
		ReportData: decoded.ReportData,
	}, nil
}

func NewEspressoSGXVerifier(l1Client *ethclient.Client, addr common.Address) (*EspressoSGXVerifier, error) {
	return &EspressoSGXVerifier{l1Client: l1Client, address: addr}, nil
}

// EnclaveReport is a legacy type used by the legacy SGX verifier.
// It's safe to define it here (it is stable and not expected to change),
// which lets us avoid importing the legacy contract bindings.
type EnclaveReport struct {
	CpuSvn     [16]byte
	MiscSelect [4]byte
	Reserved1  [28]byte
	Attributes [16]byte
	MrEnclave  [32]byte
	Reserved2  [32]byte
	MrSigner   [32]byte
	Reserved3  []byte
	IsvProdId  uint16
	IsvSvn     uint16
	Reserved4  []byte
	ReportData []byte
}
