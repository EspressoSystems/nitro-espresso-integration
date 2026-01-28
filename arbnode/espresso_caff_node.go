package arbnode

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"path"
	"path/filepath"
	"time"

	espressoClient "github.com/EspressoSystems/espresso-network/sdks/go/client"
	flag "github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"

	"github.com/offchainlabs/bold/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/arbnode/dataposter"
	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/cmd/genericconf"
	"github.com/offchainlabs/nitro/espresso/authdb"
	espresso_key_manager "github.com/offchainlabs/nitro/espresso/key-manager"
	"github.com/offchainlabs/nitro/espressostreamer"
	"github.com/offchainlabs/nitro/espressotee"
	"github.com/offchainlabs/nitro/execution/gethexec"
	"github.com/offchainlabs/nitro/util/headerreader"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type EspressoCaffNodeInitArgs struct {
	InitializeCaffNodeTags bool
	CaffNodePrivateKey     *ecdsa.PrivateKey
	CaffNodetxOpts         *bind.TransactOpts
	TeeHMAC                hash.Hash
}

type EspressoCaffNodeConfig struct {
	Enable                 bool                    `koanf:"enable"`
	HotShotUrl             string                  `koanf:"hotshot-url"`
	Namespace              uint64                  `koanf:"namespace"`
	HotshotPollingInterval time.Duration           `koanf:"hotshot-polling-interval"`
	HotshotPollingTimeout  time.Duration           `koanf:"hotshot-polling-timeout"`
	SGXVerifierAddr        string                  `koanf:"sgx-verifier-addr"`
	BatchPosterAddr        string                  `koanf:"batch-poster-addr"`
	RecordPerformance      bool                    `koanf:"record-performance"`
	WaitForFinalization    bool                    `koanf:"wait-for-finalization"`
	RequiredBlockDepth     uint64                  `koanf:"required-block-depth"`
	Dangerous              DangerousCaffNodeConfig `koanf:"dangerous"`
	TeeType                string                  `koanf:"tee-type"`

	// SGX specific config, leave empty if not using SGX
	TEEVerifierAddr string `koanf:"tee-verifier-addr"`

	// AWS Nitro Attestation Service URL
	AttestationServiceURL string `koanf:"attestation-service-url"`

	// Data poster config
	DataPoster        dataposter.DataPosterConfig `koanf:"data-poster"`
	ParentChainWallet genericconf.WalletConfig    `koanf:"parent-chain-wallet"`

	// Force Inclusion Checker
	ForceInclusionChecker ForceInclusionCheckerConfig `koanf:"force-inclusion-checker"`
	StateChecker          StateCheckerConfig          `koanf:"state-checker"`

	KeyPairAttestationsPath string `koanf:"key-pair-attestations-path"`
	SnapshotChecksum        string `koanf:"snapshot-checksum"`
	GenerateSnapshot        bool   `koanf:"generate-snapshot"`
	AuthDBBatchSize         int    `koanf:"auth-db-batch-size"`
}

func (c *EspressoCaffNodeConfig) ResolveDirectoryNames(chain string) {
	// Make wallet directories relative to chain directory if specified and not already absolute
	if len(c.KeyPairAttestationsPath) != 0 && !filepath.IsAbs(c.KeyPairAttestationsPath) {
		c.KeyPairAttestationsPath = path.Join(chain, c.KeyPairAttestationsPath)
	}
}

type DangerousCaffNodeConfig struct {
	IgnoreDatabaseHotshotBlock bool `koanf:"ignore-database-hotshot-block"`
	IgnoreDatabaseFromBlock    bool `koanf:"ignore-database-from-block"`
}

var DefaultDangerousCaffNodeConfig = DangerousCaffNodeConfig{
	IgnoreDatabaseHotshotBlock: false,
	IgnoreDatabaseFromBlock:    false,
}

var DefaultEspressoCaffNodeConfig = EspressoCaffNodeConfig{
	Enable:                 false,
	HotShotUrl:             "",
	Namespace:              0,
	HotshotPollingInterval: time.Millisecond * 100,
	HotshotPollingTimeout:  time.Minute * 2,
	SGXVerifierAddr:        "",
	BatchPosterAddr:        "",
	RecordPerformance:      false,
	// Setting these values to the default
	// values set by Arbitrum
	WaitForFinalization:     false,
	RequiredBlockDepth:      20,
	Dangerous:               DefaultDangerousCaffNodeConfig,
	KeyPairAttestationsPath: "caff_node_key_pair_attestations",
	TeeType:                 "",
	AttestationServiceURL:   "",
	TEEVerifierAddr:         "",
	DataPoster:              dataposter.DefaultDataPosterConfig,
	SnapshotChecksum:        "",
	ParentChainWallet:       DefaultBatchPosterL1WalletConfig,
	GenerateSnapshot:        false,
	AuthDBBatchSize:         10000,
	StateChecker:            DefaultStateCheckerConfig,
	ForceInclusionChecker:   DefaultEspressoForceInclusionCheckerConfig,
}

func EspressoCaffNodeConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Bool(prefix+".enable", DefaultEspressoCaffNodeConfig.Enable, "enable espresso Caff node")
	f.String(prefix+".hotshot-url", DefaultEspressoCaffNodeConfig.HotShotUrl, "Hotshot url")
	f.Uint64(prefix+".namespace", DefaultEspressoCaffNodeConfig.Namespace, "the namespace of the chain in Espresso Network, usually the chain id")
	f.Duration(prefix+".hotshot-polling-interval", DefaultEspressoCaffNodeConfig.HotshotPollingInterval, "time after a success")
	f.Duration(prefix+".hotshot-polling-timeout", DefaultEspressoCaffNodeConfig.HotshotPollingTimeout, "timeout for hotshot polling")
	f.String(prefix+".sgx-verifier-addr", DefaultEspressoCaffNodeConfig.SGXVerifierAddr, "espresso legacy SGX verifier address that is used to verify the signature of the Hotshot transactions")
	f.String(prefix+".batch-poster-addr", DefaultEspressoCaffNodeConfig.BatchPosterAddr, "batch poster address that is used to verify the signature of the Hotshot transactions")
	f.Bool(prefix+".record-performance", DefaultEspressoCaffNodeConfig.RecordPerformance, "record performance of the Caff node")
	f.Bool(prefix+".wait-for-finalization", DefaultEspressoCaffNodeConfig.WaitForFinalization, "Configures the Caff node to only produce blocks from delayed messages if they are finalized on the parent chain")
	f.Uint64(prefix+".required-block-depth", DefaultEspressoCaffNodeConfig.RequiredBlockDepth, "Configures the required block depth/number of confirmations on the parent chain that a delayed message is required to have before this Caff node will add it to it's state")
	f.String(prefix+".key-pair-attestations-path", DefaultEspressoCaffNodeConfig.KeyPairAttestationsPath, "Path to attestation documents with KMSKeyID, EncryptedPrivateKey attestations")
	f.String(prefix+".snapshot-checksum", DefaultEspressoCaffNodeConfig.SnapshotChecksum, "Configures the snapshot checksum")
	f.String(prefix+".tee-type", DefaultEspressoCaffNodeConfig.TeeType, "The Trusted Execution Environment (TEE) that Caff node is running in")
	f.String(prefix+".attestation-service-url", DefaultEspressoBatchPosterConfig.AttestationServiceURL, "URL of the attestation service to use for obtaining zk proof over  attestation")
	genericconf.WalletConfigAddOptions(prefix+".parent-chain-wallet", f, DefaultBatchPosterConfig.ParentChainWallet.Pathname)
	f.String(prefix+".tee-verifier-addr", DefaultEspressoCaffNodeConfig.TEEVerifierAddr, "Address of the EspressoTEEVerifier contract utilize for handling cross chain NFT verification")
	DangerousCaffNodeConfigAddOptions(prefix+".dangerous", f)
	f.Bool(prefix+".generate-snapshot", DefaultEspressoCaffNodeConfig.GenerateSnapshot, "Configures whether to generate a snapshot")
	f.Int(prefix+".auth-db-batch-size", DefaultEspressoCaffNodeConfig.AuthDBBatchSize, "Batch size to use when initializing auth tags in the AuthDB")
	dataposter.DataPosterConfigAddOptions(prefix+".data-poster", f, dataposter.DefaultDataPosterConfig)

	EspressoForceInclusionConfigAddOptions(prefix+".force-inclusion-checker", f)
	EspressoStateCheckerConfigAddOptions(prefix+".state-checker", f)
}

func DangerousCaffNodeConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Bool(prefix+".ignore-database-hotshot-block", DefaultDangerousCaffNodeConfig.IgnoreDatabaseHotshotBlock, "Ignores the database hotshot block and starts from the next block specified in the config by the user")
	f.Bool(prefix+".ignore-database-from-block", DefaultDangerousCaffNodeConfig.IgnoreDatabaseFromBlock, "Ignores the database from block and starts from the next block specified in the config by the user")
}

type EspressoCaffNodeConfigFetcher func() *EspressoCaffNodeConfig

type EspressoCaffNode struct {
	stopwaiter.StopWaiter

	executionEngine  *gethexec.ExecutionEngine
	espressoStreamer espressostreamer.EspressoStreamerInterface

	configFetcher EspressoCaffNodeConfigFetcher
	db            *authdb.AuthDB

	delayedMessageFetcher DelayedMessageFetcherInterface

	l1Reader *headerreader.HeaderReader

	forceInclusionChecker *ForceInclusionChecker
	stateChecker          *StateChecker

	batcherAddrMonitor *BatcherAddrMonitor
	currentBlock       *types.Block
	snapshotHandler    *EspressoSnapshotHandler
	keyManager         *espresso_key_manager.EspressoKeyManager
	dataPoster         *dataposter.DataPoster
	caffNodePrivateKey *ecdsa.PrivateKey

	streamerConfigFetcher EspressoStreamerConfigFetcher
}

func NewEspressoCaffNode(
	ctx context.Context,
	configFetcher EspressoCaffNodeConfigFetcher,
	chainDb ethdb.Database,
	execEngine *gethexec.ExecutionEngine,
	delayedBridge *DelayedBridge,
	l1Reader *headerreader.HeaderReader,
	recordPerformance bool,
	sequencerInbox *SequencerInbox,
	fatalErrChan chan error,
	stack *node.Node,
	dataPosterDB ethdb.Database,
	caffNodeInitArgs *EspressoCaffNodeInitArgs,
	streamerConfigFetcher EspressoStreamerConfigFetcher,
) (*EspressoCaffNode, error) {
	if !configFetcher().Enable {
		return nil, nil
	}

	if l1Reader == nil {
		return nil, fmt.Errorf("l1 reader is nil")
	}
	teeType, err := espressotee.FromString(configFetcher().TeeType)
	if err != nil {
		return nil, fmt.Errorf("Error parsing TEE type, %w", err)
	}

	var db authdb.AuthDB
	if configFetcher().TeeType != "" {
		log.Info("initialiing auth db with", "i", caffNodeInitArgs.InitializeCaffNodeTags)
		// if we need to initialize caff node tags, then we will disable auth reads
		db, err = authdb.NewAuthDB(chainDb, caffNodeInitArgs.TeeHMAC, caffNodeInitArgs.InitializeCaffNodeTags)
	} else {
		// Outside the tee, we need to remove tmac and also disable auth reads
		db, err = authdb.NewAuthDB(chainDb, nil, true)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create auth db: %w", err)
	}

	// For backward compatibility, the espresso streamer should be able to verify legacy where we signed
	// hotshot transactions using SGX quote. Therefore we create a SGX TEE verifier here.
	sgxVerifier, err := espressotee.NewEspressoSGXVerifier(
		l1Reader.Client(),
		common.HexToAddress(configFetcher().SGXVerifierAddr),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create espressoTEEVerifier: %w", err)
	}
	client := espressoClient.NewClient(configFetcher().HotShotUrl)

	fromBlock := streamerConfigFetcher().AddressMonitorStartL1

	if !configFetcher().Dangerous.IgnoreDatabaseFromBlock {
		fromBlock, err = authdb.ReadFromBlock(&db)
		if err != nil {
			return nil, fmt.Errorf("failed to read l1 block from db: %w", err)
		}
	}
	if fromBlock == 0 {
		fromBlock = streamerConfigFetcher().AddressMonitorStartL1
		if fromBlock == 0 {
			return nil, fmt.Errorf("fromBlock is 0, please provide a valid block number")
		}
	}

	batcherAddrMonitor := NewBatcherAddrMonitor(
		[]common.Address{common.HexToAddress(configFetcher().BatchPosterAddr)},
		&db,
		l1Reader,
		sequencerInbox.address,
		delayedBridge.fromBlock,
		fromBlock,
		streamerConfigFetcher().AddressMonitorStep,
	)
	espressoStreamer := espressostreamer.NewEspressoStreamer(configFetcher().Namespace,
		streamerConfigFetcher().HotShotBlock,
		sgxVerifier,
		client,
		recordPerformance,
		func(l1Height uint64, addr common.Address) (bool, error) {
			return batcherAddrMonitor.IsValid(ctx, addr, l1Height)
		},
		streamerConfigFetcher().TxnsPollingInterval,
	)

	delayedMessageFetcher := NewDelayedMessageFetcher(delayedBridge, l1Reader,
		configFetcher().WaitForFinalization, configFetcher().RequiredBlockDepth, fromBlock, sequencerInbox, fatalErrChan)

	seqInbox, err := bridgegen.NewSequencerInbox(sequencerInbox.address, l1Reader.Client())
	if err != nil {
		return nil, fmt.Errorf("failed to create sequencer inbox: %w", err)
	}

	forceInclusionChecker := NewForceInclusionChecker(
		&SeqInbox{seqInbox: seqInbox},
		configFetcher().ForceInclusionChecker,
		l1Reader,
		delayedMessageFetcher,
		fatalErrChan,
	)

	stateChecker := NewStateChecker(
		configFetcher().StateChecker,
		stack.Config().HTTPPort,
		fatalErrChan,
	)

	// Create a new EspressoKeyManager
	// Get the EspressoTEEVerifier address from SequencerInbox contract
	configAddress := configFetcher().TEEVerifierAddr
	var espressoTEEVerifierAddress common.Address
	// parse the espresso tee verifier address from config if it exists, otherwise read from the sequencerInbox
	// Eventually we should only read from the SequencerInbox
	if configAddress != "" && common.IsHexAddress(configAddress) {
		espressoTEEVerifierAddress = common.HexToAddress(configAddress)
	} else {
		espressoTEEVerifierAddress, err = sequencerInbox.con.EspressoTEEVerifier(&bind.CallOpts{})
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get EspressoTEEVerifier address: %w", err)
	}
	verifier := espressotee.NewEspressoTEEVerifier(espressoTEEVerifierAddress.Hex(), l1Reader.Client(), espressoTEEVerifierAddress)

	var dataPoster *dataposter.DataPoster
	var keyManager *espresso_key_manager.EspressoKeyManager
	var snapshotHandler *EspressoSnapshotHandler
	if teeType != espressotee.EMPTY {
		if caffNodeInitArgs.CaffNodetxOpts == nil {
			return nil, fmt.Errorf("non nil txOpts are required to run the Caff Node in a TEE")
		}

		dataPosterConfigFetcher := func() *dataposter.DataPosterConfig {
			dpCfg := configFetcher().DataPoster
			return &dpCfg
		}

		chainId, err := l1Reader.Client().ChainID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get chain id: %w", err)
		}

		dataPoster, err := dataposter.NewDataPoster(ctx,
			&dataposter.DataPosterOpts{
				Database:      dataPosterDB,
				HeaderReader:  l1Reader,
				Auth:          caffNodeInitArgs.CaffNodetxOpts,
				Config:        dataPosterConfigFetcher,
				ParentChainID: chainId,
				MetadataRetriever: func(ctx context.Context, blockNum *big.Int) ([]byte, error) {
					return nil, nil
				},
			})
		if err != nil {
			return nil, fmt.Errorf("failed to create data poster: %w", err)
		}

		keyManager = espresso_key_manager.NewEspressoKeyManager(verifier, dataPoster, nil, teeType, espressotee.CaffNode, caffNodeInitArgs.CaffNodePrivateKey, configFetcher().AttestationServiceURL)

	}
	initializeCaffNodeTags := false
	var caffNodePrivateKey *ecdsa.PrivateKey
	if caffNodeInitArgs != nil {
		initializeCaffNodeTags = caffNodeInitArgs.InitializeCaffNodeTags
		caffNodePrivateKey = caffNodeInitArgs.CaffNodePrivateKey
	}

	snapshotHandler = NewEspressoSnapshotHandler(&db, stack.InstanceDir(), stack.ResolvePath("l2chaindata"), initializeCaffNodeTags, keyManager, configFetcher().GenerateSnapshot, configFetcher().AuthDBBatchSize)

	return &EspressoCaffNode{
		configFetcher:         configFetcher,
		executionEngine:       execEngine,
		delayedMessageFetcher: delayedMessageFetcher,
		espressoStreamer:      espressoStreamer,
		db:                    &db,
		l1Reader:              l1Reader,
		forceInclusionChecker: forceInclusionChecker,
		stateChecker:          stateChecker,
		batcherAddrMonitor:    batcherAddrMonitor,
		snapshotHandler:       snapshotHandler,
		keyManager:            keyManager,
		dataPoster:            dataPoster,
		caffNodePrivateKey:    caffNodePrivateKey,
		streamerConfigFetcher: streamerConfigFetcher,
	}, nil
}

// peekMessage wraps the espressoStreamer.Peek() method, to handle producing delayed messages by checking they are within the nodes safety tolerance.
// Returns:
//   - MessageWithMetadataAndPos: A message, delayed or normally sequenced, that is for the next position in the chain.
//   - error: If any error is encountered during this function it is propegated to the caller.
//
// Semantics:
//
//	This function will either produce a message, or an error. When an error is produced, the messageWithMetadataAndPos will be nil.
//	If the message is populated, the error will be nil.
func (n *EspressoCaffNode) peekMessage(ctx context.Context) (*espressostreamer.MessageWithMetadataAndPos, uint64, error) {
	messageWithMetadataAndPos := n.espressoStreamer.Peek(ctx)

	if messageWithMetadataAndPos == nil {
		return nil, 0, nil
	}

	// Check if its a delayed message, if so fetch from the database
	delayedMessageToProcessIndex, err := n.executionEngine.NextDelayedMessageNumber()
	if err != nil {
		log.Error("failed to get next delayed message number", "err", err)
		return nil, 0, err
	}
	if delayedMessageToProcessIndex == messageWithMetadataAndPos.MessageWithMeta.DelayedMessagesRead-1 {
		messageWithMetadataAndPosDelayed, fromBlock, err := n.delayedMessageFetcher.processDelayedMessage(messageWithMetadataAndPos)
		if err != nil {
			log.Error("unable to get the next delayed message", "err", err)
			return nil, 0, err
		}
		return messageWithMetadataAndPosDelayed, fromBlock, nil
	}

	return messageWithMetadataAndPos, 0, nil
}

// Creates a block from the next message in the queue.
func (n *EspressoCaffNode) createBlock(ctx context.Context) (returnValue bool) {
	lastBlockHeader := n.currentBlock.Header()

	messageWithMetadataAndPos, fromBlock, err := n.peekMessage(ctx)
	if err != nil {
		log.Warn("unable to get next message", "err", err)
		return false
	}

	if messageWithMetadataAndPos == nil {
		// No message found, so we need to wait for the next message
		return false
	}

	messageWithMetadata := messageWithMetadataAndPos.MessageWithMeta

	// Get the state of the database at the last block
	statedb, err := n.executionEngine.Bc().StateAt(lastBlockHeader.Root)
	if err != nil {
		log.Error("failed to get state at last block header", "err", err)
		return false
	}

	log.Info("Initial State", "lastBlockHash", lastBlockHeader.Hash(), "lastBlockStateRoot", lastBlockHeader.Root)
	startTime := time.Now()

	// Run the Produce block function in replay mode
	// This is the core function that is used by replay.wasm to validate the block
	block, receipts, err := arbos.ProduceBlock(messageWithMetadata.Message,
		messageWithMetadata.DelayedMessagesRead,
		lastBlockHeader,
		statedb,
		n.executionEngine.Bc(),
		false,
		core.MessageReplayMode)

	if err != nil || block == nil {
		log.Error("Failed to produce block", "err", err)
		return false
	}

	blockCalcTime := time.Since(startTime)

	log.Info("Produced block", "block", block.Hash(), "blockNumber", block.Number(), "receipts", len(receipts))

	hotshotBlockNumber := n.espressoStreamer.GetCurrentEarliestHotShotBlockNumber()
	batch := n.db.NewBatch()

	// Store hotshot block num with auth tag
	if err := authdb.WriteNextHotshotBlockNum(batch, hotshotBlockNumber); err != nil {
		log.Error("failed to store NextHotshotBlockNum and its auth tag", "err", err)
		return false
	}

	// Store from block with signature if snapshot signer is configured
	// fromBlock will only be stored when we process a delayed message
	if fromBlock != 0 {
		if err := authdb.WriteFromBlock(batch, fromBlock); err != nil {
			log.Error("failed to store delayedMessageFetcherFromBlock and its auth tag", "err", err)
			return false
		}
	}

	// the `AuthDB.Put()` adds the auth tags alongside the content of block header, body, and receipts
	err = n.executionEngine.AppendBlock(block, statedb, receipts, blockCalcTime)
	if err != nil {
		log.Error("Failed to append block", "err", err)
		return false
	}

	// Write the batch to the database
	if err := batch.Write(); err != nil {
		log.Error("caff node create block failed to write block to db", "err", err)
		return false
	}

	n.currentBlock = block
	n.espressoStreamer.Advance()

	n.executionEngine.Bc().SetFinalized(block.Header())
	n.executionEngine.Bc().SetSafe(block.Header())
	n.espressoStreamer.RecordTimeDurationBetweenHotshotAndCurrentBlock(messageWithMetadataAndPos.HotshotHeight, time.Now())

	return true
}

func (n *EspressoCaffNode) GetEspressoStreamer() espressostreamer.EspressoStreamerInterface {
	return n.espressoStreamer
}

func (n *EspressoCaffNode) Start(ctx context.Context) error {
	n.StopWaiter.Start(ctx, n)

	if n.configFetcher().TeeType == "" && n.configFetcher().SnapshotChecksum != "" {
		return fmt.Errorf("espresso tee type is required when trying to verify a snapshot checksum")
	}

	err := n.snapshotHandler.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start snapshot verifier: %w", err)
	}

	if n.keyManager != nil {
		registered := n.keyManager.HasRegistered()
		if !registered {
			if err := n.keyManager.RegisterService(); err != nil {
				return err
			}
		}
	}

	err = n.espressoStreamer.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start espresso streamer: %w", err)
	}
	err = n.batcherAddrMonitor.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start batcher address monitor: %w", err)
	}
	err = n.forceInclusionChecker.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start force inclusion checker: %w", err)
	}

	if n.stateChecker != nil {
		err = n.stateChecker.Start(ctx)
		if err != nil {
			return fmt.Errorf("failed to start state checker: %w", err)
		}
	}

	// This is +1 because the current block is the block after the last processed block
	currentBlockHeader := n.executionEngine.Bc().CurrentBlock()
	currentBlock := n.executionEngine.Bc().GetBlock(currentBlockHeader.Hash(), currentBlockHeader.Number.Uint64())

	n.currentBlock = currentBlock

	currentBlockNum := currentBlockHeader.Number.Uint64() + 1
	currentMessagePos, err := n.executionEngine.BlockNumberToMessageIndex(currentBlockNum)
	if err != nil {
		return fmt.Errorf("failed to convert block number to message index: %w", err)
	}

	var nextHotshotBlock uint64

	if !n.configFetcher().Dangerous.IgnoreDatabaseHotshotBlock {
		nextHotshotBlock, err = authdb.ReadNextHotshotBlockNum(n.db)
		if err != nil {
			return fmt.Errorf("failed to read next hotshot block: %w", err)
		}
	}
	if nextHotshotBlock == 0 {
		// No next hotshot block found, so we need to start from config.CaffNodeConfig.NextHotshotBlock
		nextHotshotBlock = n.streamerConfigFetcher().HotShotBlock
		if nextHotshotBlock == 0 {
			return errors.New("no next hotshot block found in database or dangerous.ignore-database-hotshot-block is set to true, please set config.CaffNodeConfig.NextHotshotBlock")
		}
	}

	// The reason we do the reset here is because database is only initialized after Caff node is initialized
	// so if we want to read the current position from the database, we need to reset the streamer
	// during the start of the espresso streamer and caff node
	log.Info("Starting streamer at", "nextHotshotBlock", nextHotshotBlock, "currentMessagePos", currentMessagePos)
	n.espressoStreamer.Reset(uint64(currentMessagePos), nextHotshotBlock)

	// Nonce of the previous block is the number of delayed messages read
	// Check `NextDelayedMessageNumber` in execution node to confirm this
	delayedMessagesRead := n.executionEngine.Bc().CurrentBlock().Nonce.Uint64()
	// we store delayedmessagecount-1 because that is the index of the delayed message
	// that needs to be read
	n.delayedMessageFetcher.storeDelayedMessageLatestIndex(delayedMessagesRead - 1)

	log.Debug("stored delayed message count", "delayedMessagesRead", delayedMessagesRead-1)

	// Start the delayed message fetcher
	started := n.delayedMessageFetcher.Start(ctx)
	if !started {
		return fmt.Errorf("failed to start delayed message fetcher")
	}

	log.Info("started delayed message fetcher")
	log.Info("Caff Node successfully started")

	err = n.CallIterativelySafe(func(ctx context.Context) time.Duration {
		madeBlock := n.createBlock(ctx)
		if madeBlock {
			return n.configFetcher().HotshotPollingInterval
		}
		return n.streamerConfigFetcher().TxnsPollingInterval
	})
	if err != nil {
		return fmt.Errorf("failed to start node, error in createBlock: %w", err)
	}

	return nil
}

func (n *EspressoCaffNode) StopAndWait() {
	n.StopWaiter.StopAndWait()
	n.batcherAddrMonitor.StopAndWait()
	n.delayedMessageFetcher.StopAndWait()
	n.espressoStreamer.StopAndWait()
	n.forceInclusionChecker.StopAndWait()
	n.snapshotHandler.StopAndWait()
}
