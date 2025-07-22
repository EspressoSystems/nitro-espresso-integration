package arbnode

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

func TestBatcherAddrMonitor(t *testing.T) {
	initAddr1 := common.HexToAddress("0x1234567890123456789012345678901234567890")
	initAddr2 := common.HexToAddress("0x2345678901234567890123456789012345678901")
	initAddresses := []common.Address{
		initAddr1,
		initAddr2,
	}

	b := NewBatcherAddrMonitor(initAddresses, rawdb.NewMemoryDatabase())
	b.SetL1Height(100)

	// Test initial state
	t.Run("Initial State", func(t *testing.T) {
		result1 := b.GetValidAddresses(100)
		assert.Equal(t, initAddresses, result1)
		// Batcher monitor has not seen this L1 height
		result2 := b.GetValidAddresses(101)
		assert.Equal(t, []common.Address{}, result2)
	})

	// Test AddEvent
	t.Run("AddEvent", func(t *testing.T) {
		addr3 := common.HexToAddress("0x3456789012345678901234567890123456789012")
		err := b.AddBatchPosterSetEvent(50, initAddr1, false)
		Require(t, err)
		err = b.AddBatchPosterSetEvent(60, initAddr2, false)
		Require(t, err)
		err = b.AddBatchPosterSetEvent(70, addr3, true)
		Require(t, err)

		result1 := b.GetValidAddresses(40)
		assert.Equal(t, initAddresses, result1)

		result2 := b.GetValidAddresses(50)
		assert.Equal(t, 1, len(result2))
		assert.Equal(t, initAddr2, result2[0])

		result3 := b.GetValidAddresses(60)
		assert.Equal(t, 0, len(result3))

		result4 := b.GetValidAddresses(70)
		assert.Equal(t, 1, len(result4))
		assert.Equal(t, addr3, result4[0])

		result5 := b.GetValidAddresses(80)
		assert.Equal(t, 1, len(result5))
		assert.Equal(t, addr3, result5[0])

		result6 := b.GetValidAddresses(101)
		assert.Equal(t, 0, len(result6))
	})
}
