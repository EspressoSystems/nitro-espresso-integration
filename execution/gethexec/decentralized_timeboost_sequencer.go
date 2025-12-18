package gethexec

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"runtime/debug"
	"sync"
	"time"

	hotshotClient "github.com/EspressoSystems/espresso-network/sdks/go/client"
	protos "github.com/EspressoSystems/timeboost-proto/go-generated"
	"github.com/fxamacker/cbor/v2"
	flag "github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/arbitrum"
	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
	decentralized_timeboost_helpers "github.com/offchainlabs/nitro/decentralized-timeboost/helpers"
	decentralized_timeboost "github.com/offchainlabs/nitro/decentralized-timeboost/interfaces"
	decentralized_timeboost_types "github.com/offchainlabs/nitro/decentralized-timeboost/types"
	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/solgen/go/decentralizedtimeboostgen"
	"github.com/offchainlabs/nitro/util/arbmath"
	"github.com/offchainlabs/nitro/util/headerreader"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type TransactionType uint8

const (
	Normal TransactionType = iota
	Delayed
)

type sequencerState int

const (
	CatchUp sequencerState = iota
	WaitingForBlockProduction
	Running
)

type timeboostTransactionQueueItem struct {
	tx                 *types.Transaction
	txSize             int
	options            *arbitrum_types.ConditionalOptions
	roundId            uint64
	consensusTimestamp uint64
	delayedMessageRead uint64
	txType             TransactionType
}

type synchronizedTimeboostTransactionQueue struct {
	queue []timeboostTransactionQueueItem
	mutex sync.RWMutex
}

func (q *synchronizedTimeboostTransactionQueue) enqueue(item timeboostTransactionQueueItem) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.queue = append(q.queue, item)
}

func (q *synchronizedTimeboostTransactionQueue) enqueueItems(items []timeboostTransactionQueueItem) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.queue = append(q.queue, items...)
}

func (q *synchronizedTimeboostTransactionQueue) dequeue() timeboostTransactionQueueItem {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	// Remove the first element from the queue and then return it
	item := q.queue[0]
	q.queue = q.queue[1:]
	return item
}

func (q *synchronizedTimeboostTransactionQueue) Len() int {
	q.mutex.RLock()
	defer q.mutex.RUnlock()
	return len(q.queue)
}

func (q *synchronizedTimeboostTransactionQueue) Peek() *timeboostTransactionQueueItem {
	q.mutex.RLock()
	defer q.mutex.RUnlock()
	if len(q.queue) == 0 {
		return nil
	}
	return &q.queue[0]
}

type blockHeaderCache struct {
	mutex      sync.RWMutex
	blockCache map[uint64]*types.Header
	keys       []uint64
	maxSize    int
}

func (c *blockHeaderCache) Add(header *types.Header) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	blockNumber := header.Number.Uint64()

	if _, exists := c.blockCache[blockNumber]; exists {
		c.blockCache[blockNumber] = header
		return
	}

	if len(c.blockCache) >= c.maxSize {
		deleteCount := c.maxSize / 2
		for i := 0; i < deleteCount; i++ {
			delete(c.blockCache, c.keys[i])
		}
		c.keys = c.keys[deleteCount:]
	}

	c.blockCache[blockNumber] = header
	c.keys = append(c.keys, blockNumber)
}

func (c *blockHeaderCache) Get(blockNumber uint64) *types.Header {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.blockCache[blockNumber]
}

type DecentralizedTimeboostSequencer struct {
	stopwaiter.StopWaiter
	config                 DecentralizedTimeboostSequencerConfigFetcher
	txQueue                synchronizedTimeboostTransactionQueue
	execEngine             *ExecutionEngine
	l1Reader               *headerreader.HeaderReader
	txRetryQueue           synchronizedTimeboostTransactionQueue
	nonceCache             *nonceCache
	timeboostBridge        *DecentralizedTimeboostBridge
	delayedMessagesRead    uint64
	delayedSequencer       decentralized_timeboost.DecentralizedTimeboostDelayedSequencerInterface
	blockHeaderCache       *blockHeaderCache
	inclusionListsReceived uint64
	hotshotClient          *hotshotClient.MultipleNodesClient
	state                  sequencerState
	timeboostKeyManager    *decentralizedtimeboostgen.KeyManager
}

type DecentralizedTimeboostSequencerConfigFetcher func() *DecentralizedTimeboostSequencerConfig

type DecentralizedTimeboostSequencerConfig struct {
	Enable               bool          `koanf:"enable"`
	BlockRetryDuration   time.Duration `koanf:"block-retry-duration"`
	CatchupRetryDuration time.Duration `koanf:"catchup-retry-duration"`
	// TODO: - should these be configurable or should it be hardcoded?
	MaxTxDataSize                      int                                `koanf:"max-tx-data-size"`
	NonceCacheSize                     int                                `koanf:"nonce-cache-size"`
	MaxRevertGasReject                 uint64                             `koanf:"max-revert-gas-reject"`
	ParentChainFinalizationTime        time.Duration                      `koanf:"parent-chain-finalization-time"`
	MaxAcceptableTimestampDelta        time.Duration                      `koanf:"max-acceptable-timestamp-delta"`
	EnableProfiling                    bool                               `koanf:"enable-profiling"`
	DecentralizedTimeboostBridgeConfig DecentralizedTimeboostBridgeConfig `koanf:"decentralized-timeboost-bridge-config"`
	MetricTimeForBlockCreation         time.Duration                      `koanf:"metric-time-for-block-creation"`
	HotshotUrls                        []string                           `koanf:"hotshot-urls"`
}

var DefaultDecentralizedTimeboostSequencerConfig = DecentralizedTimeboostSequencerConfig{
	Enable:                             false,
	BlockRetryDuration:                 time.Millisecond * 5,
	CatchupRetryDuration:               time.Second * 5,
	MaxTxDataSize:                      95000,
	NonceCacheSize:                     1024,
	MaxRevertGasReject:                 0,
	ParentChainFinalizationTime:        64 * time.Second,
	MaxAcceptableTimestampDelta:        time.Hour,
	EnableProfiling:                    false,
	DecentralizedTimeboostBridgeConfig: DefaultDecentralizedTimeboostBridgeConfig,
	MetricTimeForBlockCreation:         time.Second * 5,
	HotshotUrls:                        []string{},
}

func DecentralizedTimeboostSequencerConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Bool(prefix+".enable", DefaultDecentralizedTimeboostSequencerConfig.Enable, "enable timeboost sequencer")
	f.Duration(prefix+".block-retry-duration", DefaultDecentralizedTimeboostSequencerConfig.BlockRetryDuration, "retry duration after failing to create a block")
	f.Duration(prefix+".catchup-retry-duration", DefaultDecentralizedTimeboostSequencerConfig.CatchupRetryDuration, "catchup retry duration after failing to create a block")
	f.Int(prefix+".max-tx-data-size", DefaultDecentralizedTimeboostSequencerConfig.MaxTxDataSize, "maximum transaction size the sequencer will accept")
	f.Int(prefix+".nonce-cache-size", DefaultDecentralizedTimeboostSequencerConfig.NonceCacheSize, "size of the tx sender nonce cache")
	f.Uint64(prefix+".max-revert-gas-reject", DefaultDecentralizedTimeboostSequencerConfig.MaxRevertGasReject, "maximum gas executed in a revert for the sequencer to reject the transaction instead of posting it (anti-DOS)")
	f.Duration(prefix+".parent-chain-finalization-time", DefaultDecentralizedTimeboostSequencerConfig.ParentChainFinalizationTime, "parent chain finalization time")
	f.Duration(prefix+".max-acceptable-timestamp-delta", DefaultDecentralizedTimeboostSequencerConfig.MaxAcceptableTimestampDelta, "maximum acceptable time difference between the local time and the latest L1 block's timestamp")
	f.Bool(prefix+".enable-profiling", DefaultDecentralizedTimeboostSequencerConfig.EnableProfiling, "enable CPU profiling and tracing")
	f.Duration(prefix+".metric-time-for-block-creation", DefaultDecentralizedTimeboostSequencerConfig.MetricTimeForBlockCreation, "time to measure the time it takes to create a block")
	f.StringArray(prefix+".hotshot-urls", DefaultDecentralizedTimeboostSequencerConfig.HotshotUrls, "hotshot urls to query")
	DecentralizedTimeboostBridgeConfigAddOptions(prefix+".decentralized-timeboost-bridge-config", f)
}

func NewDecentralizedTimeboostSequencer(
	execEngine *ExecutionEngine,
	l1Reader *headerreader.HeaderReader,
	delayedSequencer decentralized_timeboost.DecentralizedTimeboostDelayedSequencerInterface,
	configFetcher DecentralizedTimeboostSequencerConfigFetcher,
	keyManagerAddress common.Address,
) (*DecentralizedTimeboostSequencer, error) {
	client, err := hotshotClient.NewMultipleNodesClient(configFetcher().HotshotUrls)
	if err != nil {
		return nil, err
	}
	timeboostKeyManager, err := decentralizedtimeboostgen.NewKeyManager(keyManagerAddress, l1Reader.Client())
	if err != nil {
		return nil, err
	}
	return &DecentralizedTimeboostSequencer{
		config:     configFetcher,
		execEngine: execEngine,
		l1Reader:   l1Reader,
		nonceCache: newNonceCache(configFetcher().NonceCacheSize),
		timeboostBridge: &DecentralizedTimeboostBridge{
			config:     configFetcher().DecentralizedTimeboostBridgeConfig,
			grpcClient: nil,
		},
		delayedMessagesRead: 1,
		delayedSequencer:    delayedSequencer,
		blockHeaderCache: &blockHeaderCache{
			blockCache: make(map[uint64]*types.Header),
			keys:       make([]uint64, 0, 512),
			maxSize:    512,
		},
		inclusionListsReceived: 0,
		hotshotClient:          client,
		timeboostKeyManager:    timeboostKeyManager,
		state:                  Running,
	}, nil
}

func (s *DecentralizedTimeboostSequencer) createBlock(ctx context.Context) (returnValue bool) {
	// First we need to create the current list of transactions that we will process
	queueItems := make([]timeboostTransactionQueueItem, 0)
	var totalBlockSize int
	madeBlock := false
	start := time.Now()

	defer func() {
		panicErr := recover()
		if panicErr != nil {
			log.Error("sequencer block creation panicked", "panic", panicErr, "backtrace", string(debug.Stack()))
			// TODO: we should return errors to the user here
			// For now we are logging
			for _, queueItem := range queueItems {
				log.Error("Error processing transaction", "err", sequencerInternalError, "queueItem", queueItem)
			}
		}
		returnValue = madeBlock
	}()

	lastBlock := s.execEngine.bc.CurrentBlock()
	config := s.config()

outer:
	for {
		var queueItem timeboostTransactionQueueItem
		//  Transaction retry queue should only
		//  have transactions from a given round id
		if s.txRetryQueue.Len() > 0 {
			queueItem = s.txRetryQueue.dequeue()
		} else {
			// Only add transactions from the same round id or if the queue is empty
			tx := s.txQueue.Peek()
			if tx == nil {
				break
			}
			start = time.Now()
			empty := len(queueItems) == 0
			switch tx.txType {
			case Normal:
				if empty {
					queueItem = s.txQueue.dequeue()
				} else if queueItems[len(queueItems)-1].roundId == tx.roundId {
					queueItem = s.txQueue.dequeue()
				} else {
					break outer
				}
			case Delayed:
				// create block with non delayed transactions from same round first
				if !empty {
					break outer
				}

				protoBlocks, err := s.delayedSequencer.SequenceDelayedMessages(ctx, lastBlock.Number.Uint64(), tx.delayedMessageRead, tx.roundId)
				if err != nil {
					return madeBlock
				}
				s.txQueue.dequeue()
				if protoBlocks == nil {
					log.Debug("no blocks were created from processed delayed messages")
					return madeBlock
				}
				s.state = Running
				log.Info("enqueuing blocks created from delayed messages to timeboost", "blocks", len(protoBlocks))
				s.timeboostBridge.EnqueueBlocksToTimeboost(protoBlocks)
				madeBlock = true
				return madeBlock
			default:
				log.Warn("unexpected tx type, discarding", "type", tx.txType)
				s.txQueue.dequeue()
				continue
			}
		}

		// If context is done, return false
		select {
		case <-ctx.Done():
			return madeBlock
		default:
		}

		if queueItem.txSize > s.config().MaxTxDataSize {
			// This tx is too large
			// Even if its a priority item this should be skipped,
			// TODO: return the error to the user here
			log.Warn("timeboost transaction is too large", "txSize", queueItem.txSize, "maxTxDataSize", s.config().MaxTxDataSize, "txHash", queueItem.tx.Hash().Hex())
			continue
		}

		if arbmath.BigLessThan(queueItem.tx.GasFeeCap(), lastBlock.BaseFee) {
			// This tx is too low gas fee
			// TODO: return the error to the user here
			log.Warn("timeboost transaction has too low gas fee", "txSize", queueItem.txSize, "gasFeeCap", queueItem.tx.GasFeeCap(), "baseFee", lastBlock.BaseFee, "txHash", queueItem.tx.Hash().Hex())
			continue
		}

		if totalBlockSize+queueItem.txSize > s.config().MaxTxDataSize {
			// This tx would be too large to add to this batch
			log.Info("timeboost transaction is too large, adding to retry queue", "txSize", queueItem.txSize, "maxTxDataSize", s.config().MaxTxDataSize, "txHash", queueItem.tx.Hash().Hex())
			s.txRetryQueue.enqueue(queueItem)
			// End the batch here to put this tx in the next one
			break
		}
		totalBlockSize += queueItem.txSize
		queueItems = append(queueItems, queueItem)
	}

	if len(queueItems) == 0 {
		return madeBlock
	}

	s.nonceCache.Resize(config.NonceCacheSize)
	// Nonce cache is updated to indicate a new block creation has started
	s.nonceCache.BeginNewBlock()
	// Check nonces for each transaction in the queue
	queueItems = s.precheckNonces(queueItems)
	txes := make([]*types.Transaction, len(queueItems))
	// Add hooks which include pre tx filter and post tx filter
	hooks := s.makeSequencingHooks()
	hooks.ConditionalOptionsForTx = make([]*arbitrum_types.ConditionalOptions, len(queueItems))
	totalBlockSize = 0
	// Add each queue's item to the txes list and add the total block size
	for i, queueItem := range queueItems {
		txes[i] = queueItem.tx
		totalBlockSize = arbmath.SaturatingAdd(totalBlockSize, queueItem.txSize)
		hooks.ConditionalOptionsForTx[i] = queueItem.options
	}

	// if for some reason the total block size is greater than the max tx data size
	// then we need to add the transactions to the retry queue
	if totalBlockSize > config.MaxTxDataSize {
		s.txRetryQueue.enqueueItems(queueItems)
		log.Error(
			"put too many transactions in a block",
			"numTxes", len(queueItems),
			"totalBlockSize", totalBlockSize,
			"maxTxDataSize", config.MaxTxDataSize,
		)
		return madeBlock
	}

	if len(queueItems) == 0 {
		// If we are in waiting state, we want to retry immediately
		if s.state == WaitingForBlockProduction {
			madeBlock = true
			return madeBlock
		}
		return madeBlock
	}

	firstQueueItem := queueItems[0]

	// Get the consensus timestamp of the first transaction in the queue
	// It should be the same for all transactions in the queue because
	// each transaction is a part of the same round
	timestamp := firstQueueItem.consensusTimestamp
	header, err := s.l1Reader.LastHeader(ctx)
	if err != nil {
		log.Error("failed to get latest finalized block header", "err", err)
		s.txRetryQueue.enqueueItems(queueItems)
		return madeBlock
	}

	// finalized l1 block <= consensus timestamp - parent chain finalization time
	l1Block, err := s.getL1BlockNumber(ctx, header.Number.Uint64(), timestamp)
	if err != nil {
		log.Error("error getting l1 block number, adding to retry queue", "err", err)
		s.txRetryQueue.enqueueItems(queueItems)
		return madeBlock
	}

	l1IncomingMessageHeader := &arbostypes.L1IncomingMessageHeader{
		Kind:        arbostypes.L1MessageType_L2Message,
		Poster:      l1pricing.BatchPosterAddress,
		BlockNumber: l1Block.Number.Uint64(),
		Timestamp:   timestamp,
		RequestId:   nil,
		L1BaseFee:   nil,
	}

	var block *types.Block
	if config.EnableProfiling {
		block, err = s.execEngine.SequenceTransactionsWithProfiling(l1IncomingMessageHeader, txes, hooks, nil)
	} else {
		block, err = s.execEngine.SequenceTransactions(l1IncomingMessageHeader, txes, hooks, nil)
	}

	// The hooks.TxErrors should match the txes. For case where there is no error, we should have a nil error
	if err == nil && len(hooks.TxErrors) != len(txes) {
		err = fmt.Errorf("unexpected number of error results: %v vs number of txes %v", len(hooks.TxErrors), len(txes))
	}

	if errors.Is(err, execution.ErrRetrySequencer) {
		log.Warn("error sequencing transactions", "err", err)
		s.txRetryQueue.enqueueItems(queueItems)
		return madeBlock
	}

	if err != nil {
		if errors.Is(err, context.Canceled) {
			// thread closed. We'll later try to forward these messages.
			s.txRetryQueue.enqueueItems(queueItems)
			return madeBlock
		}
		for _, queueItem := range queueItems {
			// TODO: should send the error back to the user
			if s.state == WaitingForBlockProduction {
				log.Error("error sequencing transactions after catchup. this will be retried", "err", err, "txHash", queueItem.tx.Hash().Hex())
				s.txRetryQueue.enqueueItems(queueItems)
			} else {
				log.Error("error sequencing transactions", "err", err, "txHash", queueItem.tx.Hash().Hex())
			}
		}
		return madeBlock
	}

	if block != nil {
		// If any error fails, it will fail on all nodes as this block is deterministic
		// But this should not happen at this point
		protoBlock, err := s.createTimeboostProtoBlock(l1IncomingMessageHeader, block, txes, hooks.TxErrors, firstQueueItem.roundId)
		if err != nil {
			log.Error("block was failed to be converted to a proto block", "err", err)
			return madeBlock
		}

		// We dont want to delay by making an RPC call here as we want block creation to be fast, so just add it to a queue
		// The TimeboostBridge will handle retries if needed
		elapsed := time.Since(start)
		if block.NumberU64()%100 == 0 {
			log.Info("enqueuing block to timeboost", "block", block.NumberU64(), "block hash", block.Hash().Hex(), "backlog txns", len(s.txQueue.queue), "block time elapsed", elapsed)
		}
		s.timeboostBridge.EnqueueBlockToTimeboost(protoBlock)
		successfulBlocksCounter.Inc(1)
		s.nonceCache.Finalize(block)
		s.state = Running
		// Add a metric to indicate how long it took to create the block
		blockCreationTimer.Update(elapsed)
		if elapsed >= config.MetricTimeForBlockCreation {
			blockNum := block.Number()
			log.Warn("took over 5 seconds to sequence a block", "elapsed", elapsed, "numTxes", len(txes), "success", block != nil, "l2 block", blockNum)
		}
	}

	for i, err := range hooks.TxErrors {
		if err == nil {
			madeBlock = true
		}
		queueItem := queueItems[i]
		if errors.Is(err, core.ErrGasLimitReached) {
			// There's not enough gas left in the block for this tx.
			if madeBlock {
				// There was already an earlier tx in the block; retry in a fresh block.
				s.txRetryQueue.enqueue(queueItem)
				continue
			}
		}
		if errors.Is(err, core.ErrIntrinsicGas) {
			// Strip additional information, as it's incorrect due to L1 data gas.
			err = core.ErrIntrinsicGas
			log.Error("error sequencing transactions", "err", err)
		}
		var nonceError NonceError
		if errors.As(err, &nonceError) && nonceError.txNonce > nonceError.stateNonce {
			log.Error("nonce error", "err", err, "txHash", queueItem.tx.Hash().Hex())
			continue
		}
	}

	return madeBlock
}

func (s *DecentralizedTimeboostSequencer) getL1BlockNumber(ctx context.Context, startBlockNumber uint64, consensusTimestamp uint64) (*types.Header, error) {
	finalizationTime := uint64(s.config().ParentChainFinalizationTime.Seconds())
	targetTime := consensusTimestamp - finalizationTime

	blockNumber := startBlockNumber
	for {
		var header *types.Header
		if cached := s.blockHeaderCache.Get(blockNumber); cached != nil {
			header = cached
		} else {
			block, err := s.l1Reader.Client().BlockByNumber(ctx, new(big.Int).SetUint64(blockNumber))
			if err != nil {
				return nil, err
			}
			header = block.Header()
			s.blockHeaderCache.Add(header)
		}

		if header.Time <= targetTime {
			return header, nil
		}
		blockNumber--
	}
}

func (s *DecentralizedTimeboostSequencer) makeSequencingHooks() *arbos.SequencingHooks {
	return &arbos.SequencingHooks{
		PreTxFilter:             s.preTxFilter,
		PostTxFilter:            s.postTxFilter,
		DiscardInvalidTxsEarly:  true,
		TxErrors:                []error{},
		ConditionalOptionsForTx: nil,
	}
}

func (s *DecentralizedTimeboostSequencer) preTxFilter(_ *params.ChainConfig, header *types.Header, statedb *state.StateDB, _ *arbosState.ArbosState, tx *types.Transaction, options *arbitrum_types.ConditionalOptions, sender common.Address, l1Info *arbos.L1Info) error {
	if s.nonceCache.Caching() {
		stateNonce := s.nonceCache.Get(header, statedb, sender)
		err := MakeNonceError(sender, tx.Nonce(), stateNonce)
		if err != nil {
			nonceCacheRejectedCounter.Inc(1)
			return err
		}
	}

	if options != nil {
		err := options.Check(l1Info.L1BlockNumber(), header.Time, statedb)
		if err != nil {
			conditionalTxRejectedBySequencerCounter.Inc(1)
			return err
		}
		conditionalTxAcceptedBySequencerCounter.Inc(1)
	}
	return nil
}

func (s *DecentralizedTimeboostSequencer) postTxFilter(header *types.Header, statedb *state.StateDB, _ *arbosState.ArbosState, tx *types.Transaction, sender common.Address, dataGas uint64, result *core.ExecutionResult) error {
	if statedb.IsTxFiltered() {
		return state.ErrArbTxFilter
	}
	if result.Err != nil && result.UsedGas > dataGas && result.UsedGas-dataGas <= s.config().MaxRevertGasReject {
		return arbitrum.NewRevertReason(result)
	}
	newNonce := tx.Nonce() + 1
	s.nonceCache.Update(header, sender, newNonce)
	return nil
}

func (s *DecentralizedTimeboostSequencer) precheckNonces(queueItems []timeboostTransactionQueueItem) []timeboostTransactionQueueItem {
	bc := s.execEngine.bc
	latestHeader := bc.CurrentBlock()
	latestState, err := bc.StateAt(latestHeader.Root)
	if err != nil {
		log.Error("failed to get current state to pre-check nonces", "err", err)
		return queueItems
	}
	nextHeaderNumber := arbmath.BigAdd(latestHeader.Number, common.Big1)
	arbosVersion := types.DeserializeHeaderExtraInformation(latestHeader).ArbOSFormatVersion
	signer := types.MakeSigner(bc.Config(), nextHeaderNumber, latestHeader.Time, arbosVersion)
	outputQueueItems := make([]timeboostTransactionQueueItem, 0, len(queueItems))
	var nextQueueItem *timeboostTransactionQueueItem
	var queueItemsIdx int
	pendingNonces := make(map[common.Address]uint64)
	for {
		var queueItem timeboostTransactionQueueItem
		if nextQueueItem != nil {
			queueItem = *nextQueueItem
			nextQueueItem = nil
		} else if queueItemsIdx < len(queueItems) {
			queueItem = queueItems[queueItemsIdx]
			queueItemsIdx++
		} else {
			break
		}
		tx := queueItem.tx
		sender, err := types.Sender(signer, tx)
		if err != nil {
			// TODO: should send the error back to the user
			log.Warn("failed to get sender", "err", err, "txHash", tx.Hash())
			continue
		}
		stateNonce := s.nonceCache.Get(latestHeader, latestState, sender)
		pendingNonce, pending := pendingNonces[sender]
		if !pending {
			pendingNonce = stateNonce
		}
		txNonce := tx.Nonce()

		if txNonce == pendingNonce {
			// We already found a tx with pendingNonce
			// so now we increase the pendingNonce
			pendingNonces[sender] = txNonce + 1
		} else if txNonce < stateNonce || txNonce > pendingNonce {
			// It's impossible for this tx to succeed so far,
			// because its nonce is lower than the state nonce
			// or higher than the highest tx nonce we've seen.
			err := MakeNonceError(sender, txNonce, stateNonce)
			if errors.Is(err, core.ErrNonceTooHigh) {
				var nonceError NonceError
				if !errors.As(err, &nonceError) {
					log.Warn("unreachable nonce error is not nonceError")
					continue
				}
				// TODO send the error back to the user
				log.Error("failed to process transaction nonce", "err", err, "sender", sender, "txNonce", txNonce, "txHash", tx.Hash().Hex())
				continue
			} else if err != nil {
				nonceCacheRejectedCounter.Inc(1)
				log.Warn("failed to process transaction nonce", "err", err, "sender", sender, "txNonce", txNonce, "txHash", tx.Hash().Hex())
				continue
			} else {
				log.Warn("unreachable nonce err == nil condition hit in precheckNonces")
			}

		}
		outputQueueItems = append(outputQueueItems, queueItem)
	}

	return outputQueueItems
}

// Try to create proto block, this is what timeboost will create certificate over
func (s *DecentralizedTimeboostSequencer) createTimeboostProtoBlock(
	l1IncomingMessageHeader *arbostypes.L1IncomingMessageHeader,
	block *types.Block,
	txes types.Transactions,
	txErrors []error,
	roundId uint64,
) (*protos.Block, error) {
	msg, err := MessageFromTxes(l1IncomingMessageHeader, txes, txErrors)
	if err != nil {
		return nil, err
	}

	msgIdx, err := s.execEngine.BlockNumberToMessageIndex(block.NumberU64())
	if err != nil {
		return nil, err
	}
	messageWithMeta := arbostypes.MessageWithMetadata{
		Message:             msg,
		DelayedMessagesRead: block.Nonce(),
	}

	msgBytes, err := rlp.EncodeToBytes(messageWithMeta)
	if err != nil {
		return nil, err
	}
	pos := uint64(msgIdx)
	payload := decentralized_timeboost_types.MessagePayload{
		Position: pos,
		Message:  msgBytes,
	}
	encoded, err := cbor.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &protos.Block{
		Number:  pos,
		Round:   roundId,
		Payload: encoded,
	}, nil
}

// This method is called if we detect we went down, and we will do the following:
// 1.) Look for verified timeboost transactions in hotshot blocks
// 2.) See if our local state syncing from parent chain has caught up to these latest hotshot blocks
// 3.) Ensure we have inclusion lists from before the found verified timeboost transaction from hotshot
// 4.) Discard the old inclusion lists then start processing again from our queue
func (s *DecentralizedTimeboostSequencer) waitForCatchup(ctx context.Context) error {
	if s.execEngine == nil || s.execEngine.bc == nil {
		panic("engine should not be nil")
	}

	// Start from latest hotshot block
	height, err := s.hotshotClient.FetchLatestBlockHeight(ctx)
	if err != nil {
		return err
	}
	blocks := make(map[uint64]uint64)
	for {
		// Look for certified timeboost blocks
		txns, err := s.hotshotClient.FetchTransactionsInBlock(ctx, height, s.execEngine.bc.Config().ChainID.Uint64())
		if err != nil {
			log.Debug("error getting hotshot block", "err", err, "height", height)
			continue
		}
		for _, tx := range txns.Transactions {
			var body decentralized_timeboost_types.Body
			if err := cbor.Unmarshal(tx, &body); err != nil {
				log.Warn("cbor error decoding certified block", "err", err)
				continue
			}

			for _, block := range body.Blocks {
				if err := decentralized_timeboost_helpers.VerifyTimeboostBlock(&block, s.timeboostKeyManager.GetCommitteeById); err != nil {
					log.Warn("catchup error verifying timeboost block", "err", err)
					continue
				}
				blocks[block.Data.Number] = block.Data.Round
			}

			// Sequencer state will catchup from parent chain.
			currentBlock := s.execEngine.bc.CurrentBlock()
			if currentBlock == nil {
				continue
			}
			executedBlock := currentBlock.Number.Uint64()
			txn := s.txQueue.Peek()
			if txn == nil {
				continue
			}
			// Make sure local block chain state is up to date with a certified timeboost block found in hotshot
			if currentRound, exists := blocks[executedBlock]; exists && currentRound > txn.roundId {
				log.Info("local block is up to date with hotshot block", "certified block round", currentRound, "queued txn round", txn.roundId, "l2 block", executedBlock)
				for {
					txn = s.txQueue.Peek()
					if txn == nil {
						log.Info("catchup complete: queue empty", "l2 block", executedBlock, "certified block round", currentRound)
						return nil
					}
					if currentRound <= txn.roundId {
						log.Info("catchup complete: queue caught up", "next round", txn.roundId, "certified block round", currentRound, "l2 block", executedBlock)
						return nil
					}
					// We can safely discard any old inclusion lists whose round ids are less than the round ids in the certified block
					discarded := s.txQueue.dequeue()
					log.Info("discarded obsolete txn", "discarded round", discarded.roundId, "certified block round", currentRound, "l2 block", executedBlock)
				}
			}
		}
		// If we don't meet these conditions, fetch next hotshot block and try again
		height += 1
	}
}

func (s *DecentralizedTimeboostSequencer) ProcessInclusionList(ctx context.Context, inclusionList *protos.InclusionList, options *arbitrum_types.ConditionalOptions) error {
	if s.inclusionListsReceived%200 == 0 {
		log.Info("processing inclusion list", "round", inclusionList.Round, "len", len(inclusionList.EncodedTxns), "delayed messages read", inclusionList.DelayedMessagesRead)
	}
	var items []timeboostTransactionQueueItem
	for _, protoTx := range inclusionList.EncodedTxns {
		var tx types.Transaction
		if err := tx.UnmarshalBinary(protoTx.EncodedTxn); err != nil {
			log.Warn("error unmarshalling encoded transaction", "err", err)
			return err
		}
		txQueueItem := timeboostTransactionQueueItem{
			tx:                 &tx,
			txSize:             len(protoTx.EncodedTxn),
			options:            options,
			roundId:            inclusionList.Round,
			consensusTimestamp: inclusionList.ConsensusTimestamp,
			delayedMessageRead: 0,
			txType:             Normal,
		}
		items = append(items, txQueueItem)
	}
	// add delayed messages to the end
	if s.delayedMessagesRead < inclusionList.DelayedMessagesRead {
		// We will fetch the transaction when we go to make a block, so just set to nil
		txQueueItem := timeboostTransactionQueueItem{
			tx:                 nil,
			txSize:             0,
			options:            options,
			roundId:            inclusionList.Round,
			consensusTimestamp: inclusionList.ConsensusTimestamp,
			delayedMessageRead: inclusionList.DelayedMessagesRead,
			txType:             Delayed,
		}
		items = append(items, txQueueItem)
	}
	// we need to append all the items at once, otherwise the timers can be off
	// between the different nodes sequencers, where they may start to make the block
	// with only a few of the transactions
	s.txQueue.enqueueItems(items)
	s.delayedMessagesRead = inclusionList.DelayedMessagesRead
	s.inclusionListsReceived += 1
	return nil
}

func (s *DecentralizedTimeboostSequencer) Start(ctx context.Context) error {
	s.StopWaiter.Start(ctx, s)
	if s.l1Reader == nil {
		return errors.New("l1Reader is nil")
	}

	if err := s.timeboostBridge.Start(ctx, s.ProcessInclusionList); err != nil {
		return err
	}

	// Check our database to see if we restarted
	if s.execEngine.recorder != nil && s.execEngine.recorder.execEngine != nil {
		lastHeader, err := s.execEngine.recorder.execEngine.getCurrentHeader()
		if err == nil && lastHeader.Number.Uint64() > 0 {
			log.Warn("Detected sequencer was shutdown, entering catchup protocol")
			s.state = CatchUp
		}
	}

	if err := s.CallIterativelySafe(func(ctx context.Context) time.Duration {
		switch s.state {
		case CatchUp:
			err := s.waitForCatchup(ctx)
			if err != nil {
				log.Warn("error during catchup", "err", err)
				return 0
			}
			// catchup is done, now make sure we can produce a block
			s.state = WaitingForBlockProduction
			return 0
		case Running:
			if s.createBlock(ctx) {
				return 0
			}
		case WaitingForBlockProduction:
			if s.createBlock(ctx) {
				return 0
			}
			log.Info("still waiting for first block production since catchup will retry", "retry in", s.config().CatchupRetryDuration)
			return s.config().CatchupRetryDuration
		}
		return s.config().BlockRetryDuration
	}); err != nil {
		return err
	}

	headerCh := make(chan *types.Header)
	sub, err := s.l1Reader.Client().SubscribeNewHead(ctx, headerCh)
	if err != nil {
		return err
	}
	err = s.CallIterativelySafe(func(ctx context.Context) time.Duration {
		select {
		case header := <-headerCh:
			s.storeHeader(header)
		case err := <-sub.Err():
			log.Error("subscription error", "err", err)
		case <-ctx.Done():
			log.Error("context canceled", "ctx", ctx.Err())
		}
		return 0
	})
	return err
}

func (s *DecentralizedTimeboostSequencer) storeHeader(header *types.Header) {
	s.blockHeaderCache.Add(header)
}

func (s *DecentralizedTimeboostSequencer) StopAndWait() {
	s.timeboostBridge.StopAndWait()
	s.StopWaiter.StopAndWait()

	if s.txRetryQueue.Len() == 0 &&
		s.txQueue.Len() == 0 {
		return
	}

	log.Warn("Sequencer has queued items while shutting down",
		"txQueue", s.txQueue.Len(),
		"retryQueue", s.txRetryQueue.Len(),
	)
}
