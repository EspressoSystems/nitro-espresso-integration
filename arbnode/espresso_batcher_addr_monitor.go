package arbnode

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/offchainlabs/nitro/espresso/authdb"
	"github.com/offchainlabs/nitro/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/util/headerreader"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

var ownerFunctionCalledID common.Hash

// 13 minutes worth of blocks on L1 (assuming 12s block time)
const BUFFER_WINDOW = 64

func init() {
	parsedSeqInboxABI, err := bridgegen.SequencerInboxMetaData.GetAbi()
	if err != nil {
		panic(err)
	}
	ownerFunctionCalledID = parsedSeqInboxABI.Events["OwnerFunctionCalled"].ID
}

type BatcherAddrMonitorInterface interface {
	Start(ctx context.Context) error
	IsValid(ctx context.Context, batcherAddress common.Address, l1Height uint64) (bool, error)
}

type BatcherAddrMonitor struct {
	stopwaiter.StopWaiter
	// This is corresponding L1 height to the parent height.
	// If the parent chain is Ethereum, this is equal to the parent height.
	lastProcessedL1Height     uint64
	lastProcessedParentHeight uint64

	// L1 heights where batcher addresses were updated
	// This is append-only and sorted in ascending order
	eventUpdatesAt []uint64
	// validityCaches[0]: batcher addresses valid from eventUpdatesAt[0] to eventUpdatesAt[1]
	// validityCaches[i]: batcher addresses valid from eventUpdatesAt[i] to eventUpdatesAt[i+1]
	// validityCaches[last]: batcher addresses valid from eventUpdatesAt[last] to current height (inclusive)
	// Cache all the validityCaches to avoid repeated lookups. Since the batcher addresses are relatively stable,
	// this caching significantly improves performance for repeated validity checks.
	validityCaches []map[common.Address]bool
	// A batcher address becomes valid or invalid after it is included in a block that is finalized + bufferWindow old
	// This makes sure our fast finality won't be hurt by L1 lag.
	// This value should be part of consensus among all the caff nodes and batchers.
	// It is not configurable and we currently hardcode it first.
	bufferWindow uint64
	db           *authdb.AuthDB
	needsPersist bool
	// Init addresses are the addresses serve as fallback valid addresses
	initAddresses   []common.Address
	fromParentBlock uint64

	l1Reader *headerreader.HeaderReader

	seqInboxAddr      common.Address
	seqInboxInterface *bridgegen.SequencerInbox
	deployAt          uint64
	step              uint64
}

func NewBatcherAddrMonitor(
	initAddresses []common.Address,
	db *authdb.AuthDB,
	l1Reader *headerreader.HeaderReader,
	seqInboxAddr common.Address,
	deployAt uint64,
	fromParentBlock uint64,
	step uint64,
) *BatcherAddrMonitor {
	var seqInboxInterface *bridgegen.SequencerInbox
	if l1Reader != nil {
		var err error
		seqInboxInterface, err = bridgegen.NewSequencerInbox(seqInboxAddr, l1Reader.Client())
		if err != nil {
			panic(err)
		}
	}
	if fromParentBlock < deployAt+1 {
		fromParentBlock = deployAt + 1
	}
	return &BatcherAddrMonitor{
		initAddresses:             initAddresses,
		db:                        db,
		l1Reader:                  l1Reader,
		seqInboxAddr:              seqInboxAddr,
		seqInboxInterface:         seqInboxInterface,
		deployAt:                  deployAt,
		lastProcessedParentHeight: fromParentBlock - 1,
		fromParentBlock:           fromParentBlock,
		step:                      step,
		bufferWindow:              BUFFER_WINDOW,
	}
}

func (b *BatcherAddrMonitor) IsValid(ctx context.Context, batcherAddress common.Address, l1Height uint64) (bool, error) {
	if len(b.eventUpdatesAt) == 0 {
		b.addEventUpdates([]uint64{b.fromParentBlock})
		for _, addr := range b.initAddresses {
			b.validityCaches[0][addr] = true
		}
	}
	height := b.fromParentBlock - 1
	if l1Height > b.bufferWindow {
		height = l1Height - b.bufferWindow
	}
	if height > b.lastProcessedL1Height {
		return false, fmt.Errorf("batcher address monitor is lagging behind, height: %d", l1Height)
	}

	index := 0
	for i, updateHeight := range b.eventUpdatesAt {
		if updateHeight > height {
			break
		}
		index = i
	}

	cache := b.validityCaches[index]
	if valid, exists := cache[batcherAddress]; exists {
		return valid, nil
	}

	isBatcher, err := b.seqInboxInterface.IsBatchPoster(&bind.CallOpts{}, batcherAddress)
	if err != nil {
		return false, err
	}
	log.Debug("Batcher address validation", "address", batcherAddress, "isBatcher", isBatcher, "l1Height", l1Height)
	cache[batcherAddress] = isBatcher
	b.needsPersist = true

	return isBatcher, nil
}

func (b *BatcherAddrMonitor) SetParentHeight(height uint64) {
	b.lastProcessedParentHeight = height
}

func (b *BatcherAddrMonitor) SetL1Height(height uint64) {
	b.lastProcessedL1Height = height
}

func (b *BatcherAddrMonitor) GetLastProcessedParentHeight() uint64 {
	return b.lastProcessedParentHeight
}

func (b *BatcherAddrMonitor) LookupAddressUpdates(ctx context.Context, fromBlock, toBlock uint64) ([]uint64, error) {
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
	var result []uint64
	for _, log := range logs {
		l1Height := log.BlockNumber
		if b.l1Reader.IsParentChainArbitrum() {
			header, err := b.l1Reader.Client().HeaderByNumber(ctx, big.NewInt(0).SetUint64(log.BlockNumber))
			if err != nil {
				return nil, err
			}
			l1Height = types.DeserializeHeaderExtraInformation(header).L1BlockNumber
		}
		result = append(result, l1Height)
	}
	return result, nil
}

func (b *BatcherAddrMonitor) Store() error {

	newBatch := b.db.NewBatch()

	err := authdb.WriteAddresses(newBatch, b.validityCaches)
	if err != nil {
		return fmt.Errorf("failed to write addresses: %w", err)
	}

	eventsBytes, err := rlp.EncodeToBytes(b.eventUpdatesAt)
	if err != nil {
		return fmt.Errorf("failed to encode events: %w", err)
	}
	err = authdb.WriteEvents(newBatch, eventsBytes)
	if err != nil {
		return fmt.Errorf("failed to write events: %w", err)
	}

	err = authdb.WriteLastProcessedHeight(newBatch, b.lastProcessedParentHeight)
	if err != nil {
		return fmt.Errorf("failed to put last processed height: %w", err)
	}

	return newBatch.Write()
}

func (b *BatcherAddrMonitor) Restore() error {
	lastProcessedHeight, err := authdb.ReadLastProcessedHeight(b.db)
	if err != nil {
		return fmt.Errorf("failed to get last processed height: %w", err)
	}

	if lastProcessedHeight < b.fromParentBlock {
		// It is running with a higher parent block than last processed height,
		// prvious result becomes invalid
		return nil
	}
	updates, err := authdb.ReadEvents(b.db)
	if err != nil {
		return fmt.Errorf("failed to get events: %w", err)
	}

	// Parse the RLP-encoded events into []uint64
	var eventUpdates []uint64
	if len(updates) > 0 {
		if err := rlp.DecodeBytes(updates, &eventUpdates); err != nil {
			return fmt.Errorf("failed to decode events: %w", err)
		}
	}
	b.eventUpdatesAt = eventUpdates

	b.lastProcessedParentHeight = lastProcessedHeight

	addresses, err := authdb.ReadAddresses(b.db)
	if err != nil {
		return fmt.Errorf("failed to get addresses: %w", err)
	}
	b.validityCaches = addresses

	return nil
}

func (b *BatcherAddrMonitor) backfill(ctx context.Context) error {
	latestParentHeader, err := b.l1Reader.Client().HeaderByNumber(ctx, new(big.Int).SetInt64(int64(rpc.FinalizedBlockNumber)))
	if err != nil {
		return fmt.Errorf("failed to get latest parent height: %w", err)
	}
	lastProcessedHeight := b.GetLastProcessedParentHeight()

	blocksToRead := b.step
	allowedRetry := 10
	retry := 0
	latestParentHeight := latestParentHeader.Number.Uint64()
	log.Info("batcher addr monitor backfilling")
	for retry < allowedRetry {
		if lastProcessedHeight >= latestParentHeight {
			// Already backfilled to the current known latest height.
			// However, the latest height might actually be a bit behind,
			// so update it to match lastProcessedHeight before exiting.
			latestParentHeight = lastProcessedHeight
			break
		}

		log.Info("batcher addr monitor backfilling", "lastProcessedHeight", lastProcessedHeight, "latestParentHeight", latestParentHeight, "blocksToRead", blocksToRead)
		updates, err := b.LookupAddressUpdates(ctx, lastProcessedHeight+1, lastProcessedHeight+blocksToRead)
		if err != nil {
			retry++
			log.Error("failed to lookup events", "err", err)
			continue
		}
		if len(updates) > 0 {
			b.addEventUpdates(updates)
		}
		lastProcessedHeight += blocksToRead
		latestParentHeader, err = b.l1Reader.Client().HeaderByNumber(ctx, new(big.Int).SetInt64(int64(rpc.FinalizedBlockNumber)))
		if err != nil {
			retry++
			log.Error("failed to get latest parent height", "err", err)
			continue
		}
		latestParentHeight = latestParentHeader.Number.Uint64()
	}
	b.lastProcessedParentHeight = latestParentHeight
	b.lastProcessedL1Height = latestParentHeight
	if b.l1Reader.IsParentChainArbitrum() {
		b.lastProcessedL1Height = types.DeserializeHeaderExtraInformation(latestParentHeader).L1BlockNumber
	}
	log.Info("batcher addr monitor backfilled", "parentHeight", b.lastProcessedParentHeight, "l1Height", b.lastProcessedL1Height)

	return nil
}

func (b *BatcherAddrMonitor) Process(ctx context.Context) error {
	latestHeader, err := b.l1Reader.LatestFinalizedBlockHeader(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest finalized block header: %w", err)
	}
	latestBlockNumber := latestHeader.Number.Uint64()
	parentHeight := b.GetLastProcessedParentHeight()
	// The latest finalized block doesn't change
	if parentHeight >= latestBlockNumber {
		log.Debug("processing", "parentHeight", parentHeight, "latestBlockNumber", latestBlockNumber)
		return nil
	}

	newHeight := latestBlockNumber
	updates, err := b.LookupAddressUpdates(ctx, parentHeight+1, newHeight)
	log.Debug("looking up events", "from", parentHeight+1, "to", newHeight)
	if err != nil {
		return err
	}
	b.addEventUpdates(updates)
	l1Height := newHeight
	if b.l1Reader.IsParentChainArbitrum() {
		l1Height = types.DeserializeHeaderExtraInformation(latestHeader).L1BlockNumber
	}
	b.SetL1Height(l1Height)
	b.SetParentHeight(newHeight)
	if len(updates) == 0 {
		// If no events are found, we still need to update the last processed height
		batch := b.db.NewBatch()
		err = authdb.WriteLastProcessedHeight(batch, newHeight)
		if err != nil {
			return fmt.Errorf("failed to store last processed height: %w", err)
		}
		return batch.Write()
	} else if b.needsPersist {
		err := b.Store()
		if err != nil {
			return fmt.Errorf("failed to store in batcher address monitor: %w", err)
		}
		b.needsPersist = false
	}
	return nil
}

func (b *BatcherAddrMonitor) addEventUpdates(updates []uint64) {
	b.eventUpdatesAt = append(b.eventUpdatesAt, updates...)
	for i := 0; i < len(updates); i++ {
		b.validityCaches = append(b.validityCaches, make(map[common.Address]bool))
	}
}

func (b *BatcherAddrMonitor) Start(ctx context.Context) error {
	log.Info("starting the batch poster address monitor")
	b.StopWaiter.Start(ctx, b)

	err := b.Restore()
	if err != nil && !rawdb.IsDbErrNotFound(err) {
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
			case <-headerchan:
				err := b.Process(ctx)
				if err != nil {
					continue
				}
			}
		}
	})

	return nil
}

func (b *BatcherAddrMonitor) StopAndWait() {
	b.StopWaiter.StopAndWait()
}
