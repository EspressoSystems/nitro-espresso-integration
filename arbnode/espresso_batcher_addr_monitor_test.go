package arbnode

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/offchainlabs/nitro/espresso/authdb"
	"github.com/offchainlabs/nitro/util/headerreader"
)

func TestBatcherAddrMonitor(t *testing.T) {
	initAddr1 := common.HexToAddress("0x1234567890123456789012345678901234567890")
	initAddr2 := common.HexToAddress("0x2345678901234567890123456789012345678901")
	// Test initial state
	t.Run("initial state", func(t *testing.T) {
		caffDb, err := authdb.NewAuthDB(rawdb.NewMemoryDatabase(), nil, true)
		Require(t, err)

		_ = NewBatcherAddrMonitor(&caffDb, nil, common.Address{}, 0, 0, 100)
	})

	t.Run("store and restore", func(t *testing.T) {
		dummyClient := &ethclient.Client{}
		l1Reader, err := headerreader.New(context.Background(), dummyClient, nil, nil)
		Require(t, err)
		caffDb, err := authdb.NewAuthDB(rawdb.NewMemoryDatabase(), nil, true)
		Require(t, err)
		b := NewBatcherAddrMonitor(&caffDb, l1Reader, common.Address{}, 0, 0, 100)
		b.lastProcessedParentHeight = 100
		err = b.Store()
		Require(t, err)

		err = b.Restore()
		Require(t, err)

		assert.Equal(t, uint64(100), b.lastProcessedParentHeight)

	})
	t.Run("event rlp decode/encode", func(t *testing.T) {
	})

	t.Run("IsValid with no events", func(t *testing.T) {
		ctx := context.Background()
		caffDb, err := authdb.NewAuthDB(rawdb.NewMemoryDatabase(), nil, true)
		Require(t, err)

		b := NewBatcherAddrMonitor(&caffDb, nil, common.Address{}, 0, 0, 100)
		// No events have been recorded yet; IsValid should return an error.
		ok, err := b.IsValid(ctx, initAddr1, 100)
		assert.False(t, ok)
		assert.Error(t, err)
	})

	t.Run("IsValid uses cached results", func(t *testing.T) {
		ctx := context.Background()
		caffDb, err := authdb.NewAuthDB(rawdb.NewMemoryDatabase(), nil, true)
		Require(t, err)

		b := &BatcherAddrMonitor{
			bufferWindow: 0,
			// Two update points; we will target the first cache entry.
			eventUpdatesAt: []uint64{100, 200},
			results: []map[common.Address]bool{
				{initAddr1: true},
				{initAddr2: false},
			},
			// db is not used in IsValid, but keep it non-nil for completeness.
			db: &caffDb,
		}

		// height = l1Height - bufferWindow = 150, so index will resolve to 0
		// and use the first cache map, where initAddr1 is true.
		ok, err := b.IsValid(ctx, initAddr1, 150)
		Require(t, err)
		assert.True(t, ok)

		// For a height that falls into the second interval, it should use
		// the second cache map, where initAddr2 is false.
		ok, err = b.IsValid(ctx, initAddr2, 250)
		Require(t, err)
		assert.False(t, ok)
	})
}
