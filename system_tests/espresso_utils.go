package arbtest

import (
	"math/big"
	"testing"
)

func createDummyEspressoMetadata(t *testing.T) *big.Int {
	return new(big.Int).SetUint64(1)
}
