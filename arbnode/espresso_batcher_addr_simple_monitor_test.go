package arbnode

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/common"
)

func TestBatcherAddrSimpleMonitor_IsValid(t *testing.T) {
	ctx := context.Background()

	addr1Str := "0x1234567890123456789012345678901234567890"
	addr2Str := "0x2345678901234567890123456789012345678901"

	cfgs := []AddressValidRangeConfig{
		{
			Address: addr1Str,
			From:    0,
			To:      100,
		},
		{
			Address: addr2Str,
			From:    50,
			To:      150,
		},
	}

	monitor := NewBatcherAddrSimpleMonitor(cfgs)

	addr1 := common.HexToAddress(addr1Str)
	addr2 := common.HexToAddress(addr2Str)
	addr3 := common.HexToAddress("0x3456789012345678901234567890123456789012")

	t.Run("within first range", func(t *testing.T) {
		ok, err := monitor.IsValid(ctx, addr1, 50)
		assert.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("below first range", func(t *testing.T) {
		ok, err := monitor.IsValid(ctx, addr1, 0)
		assert.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("above first range", func(t *testing.T) {
		ok, err := monitor.IsValid(ctx, addr1, 101)
		assert.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("within second range", func(t *testing.T) {
		ok, err := monitor.IsValid(ctx, addr2, 100)
		assert.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("outside any range", func(t *testing.T) {
		ok, err := monitor.IsValid(ctx, addr2, 151)
		assert.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("unknown address", func(t *testing.T) {
		ok, err := monitor.IsValid(ctx, addr3, 75)
		assert.NoError(t, err)
		assert.False(t, ok)
	})
}
