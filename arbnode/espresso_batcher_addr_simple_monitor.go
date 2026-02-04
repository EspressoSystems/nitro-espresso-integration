package arbnode

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

type AddressValidRangeConfig struct {
	Address string `koanf:"address"`
	From    uint64 `koanf:"from"`
	To      uint64 `koanf:"to"`
}

type AddressValidRange struct {
	Address common.Address `koanf:"address"`
	from    uint64         `koanf:"from"`
	to      uint64         `koanf:"to"`
}

type BatcherAddrSimpleMonitor struct {
	addressValidRanges []AddressValidRange
}

func NewBatcherAddrSimpleMonitor(addressValidRanges []AddressValidRangeConfig) *BatcherAddrSimpleMonitor {
	converted := make([]AddressValidRange, 0, len(addressValidRanges))
	for _, cfg := range addressValidRanges {
		converted = append(converted, AddressValidRange{
			Address: common.HexToAddress(cfg.Address),
			from:    cfg.From,
			to:      cfg.To,
		})
	}
	return &BatcherAddrSimpleMonitor{
		addressValidRanges: converted,
	}
}

func (b *BatcherAddrSimpleMonitor) IsValid(ctx context.Context, batcherAddress common.Address, l1Height uint64) (bool, error) {
	for _, addr := range b.addressValidRanges {
		if addr.Address == batcherAddress {
			return l1Height >= addr.from && l1Height <= addr.to, nil
		}
	}
	return false, nil
}

func (b *BatcherAddrSimpleMonitor) Start(ctx context.Context) error {
	return nil
}
