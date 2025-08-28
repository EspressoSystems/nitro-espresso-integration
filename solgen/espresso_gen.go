package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func GenerateEspressoTEEContracts(modules map[string]*moduleInfo) error {
	filePathsEspressoTeeContracts, err := filepath.Glob("espresso-tee-contracts/out/EspressoTEE.sol/*.json")
	if err != nil {
		return fmt.Errorf("failed to find espresso-tee-contracts artifacts: %w", err)
	}

	espressoTEEContractsInfo := modules["espressogen"]
	if espressoTEEContractsInfo == nil {
		espressoTEEContractsInfo = &moduleInfo{}
		modules["espressogen"] = espressoTEEContractsInfo
	}

	for _, path := range filePathsEspressoTeeContracts {
		_, file := filepath.Split(path)
		name := file[:len(file)-5]

		data, err := os.ReadFile(path)
		if err != nil {
			log.Fatal("could not read", path, "for contract", name, err)
		}
		artifact := FoundryArtifact{}
		if err := json.Unmarshal(data, &artifact); err != nil {
			return fmt.Errorf("failed to parse espresso contract %s: %w", name, err)
		}
		espressoTEEContractsInfo.addArtifact(HardHatArtifact{
			ContractName: name,
			Abi:          artifact.Abi,
			Bytecode:     artifact.Bytecode.Object,
		})
	}
	return nil
}
