package arbnode

import (
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	eventKey = "espresso-batcher-addr"
)

type BatcherAddrEvent struct {
	l1Height  uint64         `koanf:"l1-height"`
	addr      common.Address `koanf:"addr"`
	isBatcher bool           `koanf:"is-batcher"`
}

type BatcherAddrMonitorInterface interface {
	AddEvent(l1 uint64, addr common.Address, isBatcher bool) error
	GetValidAddresses(l1 uint64) []common.Address
	SetL1Height(l1 uint64)
}

type BatcherAddrMonitor struct {
	l1Height uint64

	events        []BatcherAddrEvent
	db            ethdb.Database
	initAddresses []common.Address
}

func NewBatcherAddrMonitor(
	addr []common.Address,
	db ethdb.Database,
) *BatcherAddrMonitor {
	return &BatcherAddrMonitor{
		initAddresses: addr,
		db:            db,
	}
}

func (b *BatcherAddrMonitor) AddEvent(l1 uint64, addr common.Address, isBatcher bool) error {
	event := BatcherAddrEvent{
		l1Height:  l1,
		addr:      addr,
		isBatcher: isBatcher,
	}
	b.events = append(b.events, event)
	// Sort events by l1Height to ensure correct processing order.
	// Since BatcherAddr events are infrequent, the performance impact of sorting is negligible.
	sort.Slice(b.events, func(i, j int) bool {
		return b.events[i].l1Height < b.events[j].l1Height
	})
	return b.Store()
}

func (b *BatcherAddrMonitor) GetValidAddresses(l1 uint64) []common.Address {
	if l1 > b.l1Height {
		// target l1 height is greater than seen one, return empty
		return []common.Address{}
	}

	if len(b.events) == 0 || b.events[0].l1Height > l1 {
		return b.initAddresses
	}

	result := map[common.Address]bool{}
	for _, addr := range b.initAddresses {
		result[addr] = true
	}

	for _, event := range b.events {
		if event.l1Height > l1 {
			break
		}

		result[event.addr] = event.isBatcher
	}

	var validAddrs []common.Address
	for addr, isBatcher := range result {
		if isBatcher {
			validAddrs = append(validAddrs, addr)
		}
	}

	return validAddrs
}

func (b *BatcherAddrMonitor) SetL1Height(l1 uint64) {
	b.l1Height = l1
}

func (b *BatcherAddrMonitor) Store() error {
	events := b.events
	eventsBytes, err := rlp.EncodeToBytes(events)

	if err != nil {
		return fmt.Errorf("failed to encode events: %w", err)
	}

	return b.db.Put([]byte(eventKey), eventsBytes)
}

func (b *BatcherAddrMonitor) Restore() error {
	eventsBytes, err := b.db.Get([]byte(eventKey))
	if err != nil {
		return fmt.Errorf("failed to get events: %w", err)
	}

	if eventsBytes == nil {
		return nil
	}

	events := []BatcherAddrEvent{}
	err = rlp.DecodeBytes(eventsBytes, &events)
	if err != nil {
		return fmt.Errorf("failed to decode events: %w", err)
	}

	b.events = events
	return nil
}
