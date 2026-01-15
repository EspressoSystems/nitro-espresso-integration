package gethexec

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	// Protobuf imports for grpc calls
	protos "github.com/EspressoSystems/timeboost-proto/go-generated"
	flag "github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/log"

	decentralized_timeboost_api "github.com/offchainlabs/nitro/decentralized-timeboost/api"
	decentralized_timeboost "github.com/offchainlabs/nitro/decentralized-timeboost/helpers"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

const oneMb = 1024 * 1024
const minMsgSize = oneMb * 5
const maxMsgSize = oneMb * 10
const minTimeout = 3 * time.Second
const maxTimeout = 10 * time.Second

type DecentralizedTimeboostBridge struct {
	stopwaiter.StopWaiter
	config               DecentralizedTimeboostBridgeConfig
	grpcClient           protos.InternalApiClient
	blockSubmissionQueue decentralized_timeboost.SynchronizedTimeboostBlockQueue
}

type DecentralizedTimeboostBridgeConfig struct {
	ListenPort               uint16        `koanf:"listen-port"`
	ConnectionTimeout        time.Duration `koanf:"connection-timeout"`
	MaxSendMsgSize           int           `koanf:"max-send-msg-size"`
	MaxReceiveMsgSize        int           `koanf:"max-receive-msg-size"`
	BlockSubmissionTimeout   time.Duration `koanf:"block-submission-timeout"`
	InternalTimeboostGrpcUrl string        `koanf:"internal-timeboost-grpc-url"`
}

var DefaultDecentralizedTimeboostBridgeConfig = DecentralizedTimeboostBridgeConfig{
	ListenPort:               55000,            // Default listen port that timeboost will try and connect to
	ConnectionTimeout:        5 * time.Second,  // Max time for grpc connection timeboost
	MaxSendMsgSize:           5 * 1024 * 1024,  // Max msg receive size from timeboost
	MaxReceiveMsgSize:        5 * 1024 * 1024,  // Max msg send size to timeboost
	BlockSubmissionTimeout:   5 * time.Second,  // Max timeout when sending block to timeboost
	InternalTimeboostGrpcUrl: "localhost:5000", // Timeboost grpc server url
}

func DecentralizedTimeboostBridgeConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Uint16(prefix+".listen-port", DefaultDecentralizedTimeboostBridgeConfig.ListenPort, "timeboost inclusion listener listen port")
	f.Duration(prefix+".connection-timeout", DefaultDecentralizedTimeboostBridgeConfig.ConnectionTimeout, "timeboost inclusion list connection timeout")
	f.Int(prefix+".max-send-msg-size", DefaultDecentralizedTimeboostBridgeConfig.MaxSendMsgSize, "timeboost inclusion list send message size")
	f.Int(prefix+".max-receive-msg-size", DefaultDecentralizedTimeboostBridgeConfig.MaxReceiveMsgSize, "timeboost inclusion receive message size")
	f.Duration(prefix+".block-submission-timeout", DefaultDecentralizedTimeboostBridgeConfig.BlockSubmissionTimeout, "sending block to timeboost connection timeout")
	f.String(prefix+".internal-timeboost-grpc-url", DefaultDecentralizedTimeboostBridgeConfig.InternalTimeboostGrpcUrl, "timeboost grpc server url")
}

func NewDecentralizedTimeboostBridge(config DecentralizedTimeboostBridgeConfig) (*DecentralizedTimeboostBridge, error) {
	return &DecentralizedTimeboostBridge{
		config:     config,
		grpcClient: nil,
	}, nil
}

// Add block to submission queue, block submitter thread will pick it up
func (b *DecentralizedTimeboostBridge) EnqueueBlockToTimeboost(block *protos.Block) {
	b.blockSubmissionQueue.Enqueue(block)
}

// Add blocks to submission queue, block submitter thread will pick it up
func (b *DecentralizedTimeboostBridge) EnqueueBlocksToTimeboost(blocks []*protos.Block) {
	b.blockSubmissionQueue.EnqueueBlocks(blocks)
}

// Send block to timeboost who will get certificate over the block hash and send transaction to hotshot
func (b *DecentralizedTimeboostBridge) blockSubmitter(timeout *time.Duration) time.Duration {
	if block := b.blockSubmissionQueue.Peek(); block != nil {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		if _, err := b.grpcClient.SubmitBlock(ctx, block); err != nil {
			log.Error("failed to submit block to timeboost through grpc endpoint", "err", err, "resubmit time", *timeout, "backlog", b.blockSubmissionQueue.Len())
			return *timeout
		}
		b.blockSubmissionQueue.Dequeue()
	}
	return 0
}

func (b *DecentralizedTimeboostBridge) Start(
	ctx context.Context,
	processInclusionList func(context.Context, *protos.InclusionList, *arbitrum_types.ConditionalOptions) error,
	processTimeboostState func(ctx context.Context, catchupRound *protos.TimeboostState),
) error {
	if _, err := url.ParseRequestURI(b.config.InternalTimeboostGrpcUrl); err != nil {
		panic("timeboost grpc url must be a valid url")
	}
	if b.config.MaxSendMsgSize < minMsgSize || b.config.MaxSendMsgSize > maxMsgSize {
		panic("max send message size should be between 5 and 10 mb")
	}
	if b.config.MaxReceiveMsgSize < minMsgSize || b.config.MaxReceiveMsgSize > maxMsgSize {
		panic("max receive message size should be bettern 5 and 10 mb")
	}
	if b.config.ConnectionTimeout < minTimeout || b.config.ConnectionTimeout > maxTimeout {
		panic("connection timeout should be between 3 and 10 seconds")
	}
	if b.config.BlockSubmissionTimeout < minTimeout || b.config.BlockSubmissionTimeout > maxTimeout {
		panic("block submission timeout should be between 3 and 10 seconds")
	}

	b.StopWaiter.Start(ctx, b)

	// Grpc connection to timeboost for block submission
	grpcConn, err := grpc.NewClient(b.config.InternalTimeboostGrpcUrl, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("Failed to connect to gRPC server", "err", err)
		return err
	}
	b.grpcClient = protos.NewInternalApiClient(grpcConn)

	// Grpc server for inclusion list
	b.LaunchThread(func(ctx context.Context) {
		addr := fmt.Sprintf(":%d", b.config.ListenPort)
		lis, err := net.Listen("tcp", addr)
		if err != nil {
			panic(err)
		}
		server := grpc.NewServer(
			grpc.MaxRecvMsgSize(b.config.MaxReceiveMsgSize),
			grpc.MaxSendMsgSize(b.config.MaxSendMsgSize),
			grpc.ConnectionTimeout(b.config.ConnectionTimeout),
		)
		protos.RegisterForwardApiServer(server, &decentralized_timeboost_api.ForwardService{
			ProcessInclusionList:  processInclusionList,
			ProcessTimeboostState: processTimeboostState,
		})
		go func() {
			<-ctx.Done()
			log.Info("Shutting down gRPC server...")
			server.GracefulStop()
		}()
		if err = server.Serve(lis); err != nil {
			panic(err)
		}
	})

	timeout := &b.config.BlockSubmissionTimeout
	err = b.CallIterativelySafe(func(ctx context.Context) time.Duration {
		return b.blockSubmitter(timeout)
	})
	if err != nil {
		return err
	}
	return nil
}

func (l *DecentralizedTimeboostBridge) StopAndWait() {
	l.StopWaiter.StopAndWait()
}
