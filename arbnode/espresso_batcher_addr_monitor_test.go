package arbnode

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestBatcherAddrMonitor(t *testing.T) {
	initAddr1 := common.HexToAddress("0x1234567890123456789012345678901234567890")
	initAddr2 := common.HexToAddress("0x2345678901234567890123456789012345678901")
	initAddresses := []common.Address{
		initAddr1,
		initAddr2,
	}

	// Test initial state
	t.Run("initial state", func(t *testing.T) {
		b := NewBatcherAddrMonitor(initAddresses, rawdb.NewMemoryDatabase(), nil, common.Address{}, 0)
		b.SetL1Height(100)
		result1 := b.GetValidAddresses(100)
		assert.Equal(t, initAddresses, result1)
		// Batcher monitor has not seen this L1 height
		result2 := b.GetValidAddresses(101)
		assert.Equal(t, []common.Address{}, result2)
	})

	// Test AddEvent
	t.Run("add events and get valid addresses", func(t *testing.T) {
		b := NewBatcherAddrMonitor(initAddresses, rawdb.NewMemoryDatabase(), nil, common.Address{}, 0)
		b.SetL1Height(100)
		addr3 := common.HexToAddress("0x3456789012345678901234567890123456789012")
		err := b.AddBatchPosterSetEvents([]BatcherAddrEvent{
			{50, 50, initAddr1, false},
			{60, 60, initAddr2, false},
			{70, 70, addr3, true},
		})
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

	t.Run("store and restore", func(t *testing.T) {
		b := NewBatcherAddrMonitor(initAddresses, rawdb.NewMemoryDatabase(), nil, common.Address{}, 0)
		// only contain the init addresses
		err := b.Store()
		Require(t, err)

		// empty the init addresses
		b.initAddresses = []common.Address{}
		err = b.Restore()
		Require(t, err)

		assert.Equal(t, initAddresses, b.initAddresses)
		assert.Equal(t, []BatcherAddrEvent{}, b.events)

		events := []BatcherAddrEvent{
			{50, 50, initAddr1, false},
			{60, 60, initAddr2, false},
		}
		err = b.AddBatchPosterSetEvents(events)
		Require(t, err)
		err = b.Store()
		Require(t, err)

		b.cached = true
		b.cachedAddresses = initAddresses
		b.initAddresses = []common.Address{}
		b.events = []BatcherAddrEvent{}

		err = b.Restore()
		Require(t, err)

		assert.Equal(t, initAddresses, b.initAddresses)
		assert.Equal(t, events, b.events)
		assert.Equal(t, false, b.cached)
		assert.Equal(t, []common.Address{}, b.cachedAddresses)
	})
	t.Run("event rlp decode/encode", func(t *testing.T) {
		events := []BatcherAddrEvent{
			{50, 50, initAddr1, false},
			{60, 60, initAddr2, false},
		}
		encoded, err := rlp.EncodeToBytes(events)
		Require(t, err)
		var decoded []BatcherAddrEvent
		err = rlp.DecodeBytes(encoded, &decoded)
		Require(t, err)
		assert.Equal(t, events, decoded)
	})
}
