package arbnode

import (
	"context"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/offchainlabs/bold/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/util/dbutil"
	"github.com/offchainlabs/nitro/util/headerreader"
)

const (
	eventKey         = "espresso-batcher-addr-event"
	initAddressesKey = "espresso-batcher-addr-init-addresses"
)

var batchPosterSetID common.Hash

func init() {
	parsedSeqInboxABI, err := bridgegen.SequencerInboxMetaData.GetAbi()
	if err != nil {
		panic(err)
	}
	batchPosterSetID = parsedSeqInboxABI.Events["BatchPosterSet"].ID
}

// `BatchPosterSet` event
type BatcherAddrEvent struct {
	// From this L1 height, the batcher address becomes functional or non-functional
	L1Height     uint64         `koanf:"l1-height"`
	ParentHeight uint64         `koanf:"parent-height"`
	Addr         common.Address `koanf:"addr"`
	IsBatcher    bool           `koanf:"is-batcher"`
}

type BatcherAddrMonitor struct {
	// This is corresponding L1 height to the parent height.
	// If the parent chain is Ethereum, this is equal to the parent height.
	l1Height          uint64
	lastEventL1Height uint64
	parentHeight      uint64

	// Cache for the latest valid addresses.
	// Since batcher address changes are infrequent and callers typically
	// process HotShot blocks sequentially, caching improves performance.
	cached          bool
	cachedAddresses []common.Address

	events        []BatcherAddrEvent
	db            ethdb.Database
	initAddresses []common.Address

	l1Reader *headerreader.HeaderReader

	seqInboxAddr      common.Address
	seqInboxInterface *bridgegen.SequencerInbox
}

func NewBatcherAddrMonitor(
	addr []common.Address,
	db ethdb.Database,
	l1Reader *headerreader.HeaderReader,
	seqInboxAddr common.Address,
) *BatcherAddrMonitor {
	return &BatcherAddrMonitor{
		initAddresses: addr,
		db:            db,
		l1Reader:      l1Reader,
		seqInboxAddr:  seqInboxAddr,
	}
}

func (b *BatcherAddrMonitor) AddBatchPosterSetEvents(events []BatcherAddrEvent) error {
	if len(events) == 0 {
		return nil
	}
	b.events = append(b.events, events...)
	// Sort events by l1Height to ensure correct processing order.
	// Since BatcherAddr events are infrequent, the performance impact of sorting is negligible.
	sort.Slice(b.events, func(i, j int) bool {
		return b.events[i].L1Height < b.events[j].L1Height
	})
	b.lastEventL1Height = b.events[len(b.events)-1].L1Height
	b.cached = false
	return b.Store()
}

func (b *BatcherAddrMonitor) GetValidAddresses(l1 uint64) []common.Address {
	if l1 > b.l1Height {
		// If the target L1 height is greater than the latest known L1 height,
		// return an empty slice. The caller should wait until the monitor has
		// observed at least this L1 height before calling this function.
		return []common.Address{}
	}

	if len(b.events) == 0 || b.events[0].L1Height > l1 {
		return b.initAddresses
	}

	latestCachedWindow := l1 >= b.lastEventL1Height && l1 <= b.l1Height

	if b.cached && latestCachedWindow {
		return b.cachedAddresses
	}

	result := map[common.Address]bool{}
	for _, addr := range b.initAddresses {
		result[addr] = true
	}

	for _, event := range b.events {
		if event.L1Height > l1 {
			break
		}

		result[event.Addr] = event.IsBatcher
	}

	var validAddrs []common.Address
	for addr, isBatcher := range result {
		if isBatcher {
			validAddrs = append(validAddrs, addr)
		}
	}

	if latestCachedWindow {
		b.cached = true
		b.cachedAddresses = validAddrs
	}

	return validAddrs
}

func (b *BatcherAddrMonitor) SetParentHeight(height uint64) {
	b.parentHeight = height
}

func (b *BatcherAddrMonitor) SetL1Height(height uint64) {
	b.l1Height = height
}

func (b *BatcherAddrMonitor) GetConfirmedParentHeight() uint64 {
	if b.parentHeight > 0 {
		return b.parentHeight
	}

	if len(b.events) > 0 {
		return b.events[len(b.events)-1].ParentHeight
	}
	return 0
}

func (b *BatcherAddrMonitor) LookupEvents(ctx context.Context, fromBlock, toBlock uint64) ([]BatcherAddrEvent, error) {
	from := big.NewInt(0).SetUint64(fromBlock)
	to := big.NewInt(0).SetUint64(toBlock)
	query := ethereum.FilterQuery{
		BlockHash: nil,
		FromBlock: from,
		ToBlock:   to,
		Addresses: []common.Address{b.seqInboxAddr},
		Topics:    [][]common.Hash{{batchPosterSetID}},
	}
	logs, err := b.l1Reader.Client().FilterLogs(ctx, query)
	if err != nil {
		return nil, err
	}
	return b.logsToBatcherAddrEvents(ctx, logs)
}

func (b *BatcherAddrMonitor) logsToBatcherAddrEvents(ctx context.Context, logs []types.Log) ([]BatcherAddrEvent, error) {
	if len(logs) == 0 {
		return nil, nil
	}
	events := make([]BatcherAddrEvent, 0, len(logs))
	for _, ethLog := range logs {
		e, err := b.seqInboxInterface.ParseBatchPosterSet(ethLog)
		if err != nil {
			return nil, err
		}
		l1Height := ethLog.BlockNumber
		if b.l1Reader.IsParentChainArbitrum() {
			header, err := b.l1Reader.Client().HeaderByNumber(ctx, big.NewInt(0).SetUint64(ethLog.BlockNumber))
			if err != nil {
				return nil, err
			}
			l1Height = types.DeserializeHeaderExtraInformation(header).L1BlockNumber
		}

		event := BatcherAddrEvent{
			Addr:         e.BatchPoster,
			IsBatcher:    e.IsBatchPoster,
			L1Height:     l1Height,
			ParentHeight: ethLog.BlockNumber,
		}
		events = append(events, event)
	}
	return events, nil
}

func (b *BatcherAddrMonitor) Store() error {
	eventsBytes, err := rlp.EncodeToBytes(b.events)
	if err != nil {
		return fmt.Errorf("failed to encode events: %w", err)
	}

	initAddressesBytes, err := rlp.EncodeToBytes(b.initAddresses)
	if err != nil {
		return fmt.Errorf("failed to encode init addresses: %w", err)
	}

	newBatch := b.db.NewBatch()

	err = newBatch.Put([]byte(eventKey), eventsBytes)
	if err != nil {
		return fmt.Errorf("failed to put events: %w", err)
	}

	err = newBatch.Put([]byte(initAddressesKey), initAddressesBytes)
	if err != nil {
		return fmt.Errorf("failed to put init addresses: %w", err)
	}

	return newBatch.Write()
}

func (b *BatcherAddrMonitor) Restore() error {
	initAddressesBytes, err := b.db.Get([]byte(initAddressesKey))
	if err != nil && !dbutil.IsErrNotFound(err) {
		return fmt.Errorf("failed to get init addresses: %w", err)
	}

	if initAddressesBytes != nil {
		err = rlp.DecodeBytes(initAddressesBytes, &b.initAddresses)
		if err != nil {
			return fmt.Errorf("failed to decode init addresses: %w", err)
		}
	} else {
		b.initAddresses = []common.Address{}
	}

	eventsBytes, err := b.db.Get([]byte(eventKey))
	if err != nil && !dbutil.IsErrNotFound(err) {
		return fmt.Errorf("failed to get events: %w", err)
	}

	if eventsBytes != nil {
		var events []BatcherAddrEvent
		err = rlp.DecodeBytes(eventsBytes, &events)
		if err != nil {
			return fmt.Errorf("failed to decode events: %w", err)
		}
		b.events = events
		b.cached = false
		b.cachedAddresses = []common.Address{}
		if len(events) > 0 {
			b.lastEventL1Height = events[len(events)-1].L1Height
		}
	} else {
		b.events = []BatcherAddrEvent{}
		b.cached = false
		b.cachedAddresses = []common.Address{}
		b.lastEventL1Height = 0
	}
	return nil
}

func (b *BatcherAddrMonitor) backfill(ctx context.Context) error {
	curentParentHeight := b.GetConfirmedParentHeight()
	latestParentHeight, err := b.l1Reader.Client().HeaderByNumber(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to get latest parent height: %w", err)
	}

	blocksToRead := 100
	for {
		if curentParentHeight >= latestParentHeight.Number.Uint64()-uint64(blocksToRead) {
			break
		}

		events, err := b.LookupEvents(ctx, curentParentHeight, curentParentHeight+uint64(blocksToRead))
		if err != nil {
			return fmt.Errorf("failed to lookup events: %w", err)
		}
		err = b.AddBatchPosterSetEvents(events)
		if err != nil {
			return fmt.Errorf("failed to add events: %w", err)
		}
		curentParentHeight += uint64(blocksToRead)
		latestParentHeight, err = b.l1Reader.Client().HeaderByNumber(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to get latest parent height: %w", err)
		}
	}
	return nil
}

func (b *BatcherAddrMonitor) Start(ctx context.Context) error {
	err := b.Restore()
	if err != nil && !dbutil.IsErrNotFound(err) {
		return fmt.Errorf("failed to restore batcher address monitor: %w", err)
	}

	err = b.backfill(ctx)
	if err != nil {
		return fmt.Errorf("failed to backfill batcher address monitor: %w", err)
	}

	headerchan, unsubscribe := b.l1Reader.Subscribe(false)

	go func() {
		for {
			select {
			case <-ctx.Done():
				unsubscribe()
				return
			case header := <-headerchan:
				parentHeight := b.GetConfirmedParentHeight()
				newHeight := header.Number.Uint64()
				// search for events
				events, err := b.LookupEvents(ctx, parentHeight, newHeight)
				if err != nil {
					log.Error("Failed to search for events", "err", err)
					continue
				}
				err = b.AddBatchPosterSetEvents(events)
				if err != nil {
					log.Error("Failed to add events", "err", err)
					continue
				}
				l1Height := newHeight
				if b.l1Reader.IsParentChainArbitrum() {
					l1Height = types.DeserializeHeaderExtraInformation(header).L1BlockNumber
				}
				b.SetL1Height(l1Height)
				b.SetParentHeight(newHeight)
			}
		}
	}()

	return nil
}
