package arbnode

import (
	"context"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/offchainlabs/bold/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/util/dbutil"
	"github.com/offchainlabs/nitro/util/headerreader"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

const (
	eventKey         = "espresso-batcher-addr-event"
	initAddressesKey = "espresso-batcher-addr-init-addresses"
)

var ownerFunctionCalledID common.Hash
var seqInboxABI abi.ABI

func init() {
	parsedSeqInboxABI, err := bridgegen.SequencerInboxMetaData.GetAbi()
	if err != nil {
		panic(err)
	}
	seqInboxABI = *parsedSeqInboxABI
	ownerFunctionCalledID = parsedSeqInboxABI.Events["OwnerFunctionCalled"].ID
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
	stopwaiter.StopWaiter
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

	events []BatcherAddrEvent
	db     ethdb.Database

	// Init addresses are the addresses that were set as batcher when the rollup was deployed.
	initAddresses []common.Address

	l1Reader *headerreader.HeaderReader

	seqInboxAddr      common.Address
	seqInboxInterface *bridgegen.SequencerInbox
	deployAt          uint64
}

func NewBatcherAddrMonitor(
	initAddresses []common.Address,
	db ethdb.Database,
	l1Reader *headerreader.HeaderReader,
	seqInboxAddr common.Address,
	deployAt uint64,
) *BatcherAddrMonitor {
	seqInboxInterface, err := bridgegen.NewSequencerInbox(seqInboxAddr, l1Reader.Client())
	if err != nil {
		panic(err)
	}
	return &BatcherAddrMonitor{
		initAddresses:     initAddresses,
		db:                db,
		l1Reader:          l1Reader,
		seqInboxAddr:      seqInboxAddr,
		seqInboxInterface: seqInboxInterface,
		deployAt:          deployAt,
		parentHeight:      deployAt,
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

	// If the target L1 height is within the latest cached window, return the cached result.
	// In a practical scenario, this is the most common case. Here means that during the time
	// from `lastEventL1Height` to `l1Height`, the `events` are not changed. It is not needed to
	// calculate valid batcher addresses.
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
	return b.parentHeight
}

func (b *BatcherAddrMonitor) LookupEvents(ctx context.Context, fromBlock, toBlock uint64) ([]BatcherAddrEvent, error) {
	from := big.NewInt(0).SetUint64(fromBlock)
	to := big.NewInt(0).SetUint64(toBlock)
	query := ethereum.FilterQuery{
		BlockHash: nil,
		FromBlock: from,
		ToBlock:   to,
		Addresses: []common.Address{b.seqInboxAddr},
		Topics: [][]common.Hash{
			{ownerFunctionCalledID},
			{common.BigToHash(big.NewInt(1))},
		},
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
	events := []BatcherAddrEvent{}
	for _, ethLog := range logs {
		l1Height := ethLog.BlockNumber
		if b.l1Reader.IsParentChainArbitrum() {
			header, err := b.l1Reader.Client().HeaderByNumber(ctx, big.NewInt(0).SetUint64(ethLog.BlockNumber))
			if err != nil {
				return nil, err
			}
			l1Height = types.DeserializeHeaderExtraInformation(header).L1BlockNumber
		}
		txHash := ethLog.TxHash
		tx, _, err := b.l1Reader.Client().TransactionByHash(ctx, txHash)
		if err != nil {
			return nil, err
		}
		// Parse data to get arguments from tx
		data := tx.Data()
		args, err := seqInboxABI.Methods["setIsBatchPoster"].Inputs.Unpack(data[4:])
		if err != nil {
			return nil, err
		}
		batchPoster, ok := args[0].(common.Address)
		if !ok {
			return nil, fmt.Errorf("failed to parse a log: invalid batch poster address")
		}
		isBatcher, ok := args[1].(bool)
		if !ok {
			return nil, fmt.Errorf("failed to parse a log: invalid isBatchPoster")
		}

		event := BatcherAddrEvent{
			Addr:         batchPoster,
			IsBatcher:    isBatcher,
			L1Height:     l1Height,
			ParentHeight: ethLog.BlockNumber,
		}
		log.Info("adding event for batch poster updates", "event", event)
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
	latestParentHeader, err := b.l1Reader.Client().HeaderByNumber(ctx, new(big.Int).SetInt64(int64(rpc.FinalizedBlockNumber)))
	if err != nil {
		return fmt.Errorf("failed to get latest parent height: %w", err)
	}
	currentParentHeight := b.GetConfirmedParentHeight()

	if currentParentHeight == b.deployAt {
		for _, addr := range b.initAddresses {
			isBatcher, err := b.seqInboxInterface.IsBatchPoster(&bind.CallOpts{}, addr)
			if err != nil {
				return fmt.Errorf("failed to get batcher status: %w", err)
			}
			if !isBatcher {
				return fmt.Errorf("init address %s is not a batcher", addr)
			}
		}

		currentParentHeight = b.deployAt + 1
		b.parentHeight = currentParentHeight
	}

	blocksToRead := uint64(100)
	allowedRetry := 10
	retry := 0
	latestParentHeight := latestParentHeader.Number.Uint64()
	log.Info("batcher addr monitor backfilling")
	for retry < allowedRetry {
		if currentParentHeight >= latestParentHeight {
			break
		}

		events, err := b.LookupEvents(ctx, currentParentHeight+1, currentParentHeight+blocksToRead)
		if err != nil {
			retry++
			log.Error("failed to lookup events", "err", err)
			continue
		}
		err = b.AddBatchPosterSetEvents(events)
		if err != nil {
			retry++
			log.Error("failed to add events", "err", err)
			continue
		}
		currentParentHeight += blocksToRead
		latestParentHeader, err = b.l1Reader.Client().HeaderByNumber(ctx, new(big.Int).SetInt64(int64(rpc.FinalizedBlockNumber)))
		if err != nil {
			retry++
			log.Error("failed to get latest parent height", "err", err)
			continue
		}
		latestParentHeight = latestParentHeader.Number.Uint64()
	}
	b.parentHeight = latestParentHeight
	b.l1Height = latestParentHeight
	if b.l1Reader.IsParentChainArbitrum() {
		b.l1Height = types.DeserializeHeaderExtraInformation(latestParentHeader).L1BlockNumber
	}
	log.Info("batcher addr monitor backfilled", "parentHeight", b.parentHeight, "l1Height", b.l1Height)

	return nil
}

func (b *BatcherAddrMonitor) Process(ctx context.Context) error {
	finalizedHeader, err := b.l1Reader.LatestFinalizedBlockHeader(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest finalized block number: %w", err)
	}
	finalizedBlockNr := finalizedHeader.Number.Uint64()
	parentHeight := b.GetConfirmedParentHeight()
	// The latest finalized block doesn't change
	if parentHeight >= finalizedBlockNr {
		log.Info("processing", "parentHeight", parentHeight, "finalizedBlockNr", finalizedBlockNr)
		return nil
	}

	newHeight := finalizedBlockNr
	events, err := b.LookupEvents(ctx, parentHeight+1, newHeight)
	log.Info("looking up events", "from", parentHeight+1, "to", newHeight)
	if err != nil {
		return err
	}
	err = b.AddBatchPosterSetEvents(events)
	if err != nil {
		return err
	}
	l1Height := newHeight
	if b.l1Reader.IsParentChainArbitrum() {
		l1Height = types.DeserializeHeaderExtraInformation(finalizedHeader).L1BlockNumber
	}
	b.SetL1Height(l1Height)
	b.SetParentHeight(newHeight)
	return nil
}

func (b *BatcherAddrMonitor) GetEvents() []BatcherAddrEvent {
	return b.events
}

func (b *BatcherAddrMonitor) Start(ctx context.Context) error {
	log.Info("starting the batch poster address monitor")
	b.StopWaiter.Start(ctx, b)
	err := b.Restore()
	if err != nil && !dbutil.IsErrNotFound(err) {
		return fmt.Errorf("failed to restore batcher address monitor: %w", err)
	}

	err = b.backfill(ctx)
	if err != nil {
		return fmt.Errorf("failed to backfill batcher address monitor: %w", err)
	}

	headerchan, unsubscribe := b.l1Reader.Subscribe(false)

	b.LaunchThread(func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				unsubscribe()
				return
			case _ = <-headerchan:
				err := b.Process(ctx)
				if err != nil {
					log.Error("failed to process", "err", err)
					continue
				}
			}
		}
	})

	return nil
}
