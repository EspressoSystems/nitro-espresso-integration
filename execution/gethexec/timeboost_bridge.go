package gethexec

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"

	// Protobuf imports for grpc calls
	protos "github.com/EspressoSystems/timeboost-proto/go-generated"
	flag "github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type TimeboostBridge struct {
	stopwaiter.StopWaiter
	config     TimeboostBridgeConfig
	grpcClient protos.InternalApiClient
	blockQueue synchronizedTimeboostBlockQueue
}

type TimeboostBridgeConfig struct {
	ListenPort               uint16        `koanf:"listen-port"`
	ConnectionTimeout        time.Duration `koanf:"connection-timeout"`
	MaxSendMsgSize           int           `koanf:"max-send-msg-size"`
	MaxReceiveMsgSize        int           `koanf:"max-receive-msg-size"`
	BlockSubmitTimeout       time.Duration `koanf:"block-submit-timeout"`
	InternalTimeboostGrpcUrl string        `koanf:"internal-timeboost-grpc-url"`
}

var DefaultTimeboostBridgeConfig = TimeboostBridgeConfig{
	ListenPort:               55000,            // Default listen port that timeboost will try and connect to
	ConnectionTimeout:        5 * time.Second,  // Max time for grpc connection timeboost
	MaxSendMsgSize:           5 * 1024 * 1024,  // Max msg receive size from timeboost
	MaxReceiveMsgSize:        5 * 1024 * 1024,  // Max msg send size to timeboost
	BlockSubmitTimeout:       5 * time.Second,  // Max timeout when sending block to timeboost
	InternalTimeboostGrpcUrl: "localhost:5000", // Timeboost grpc server url
}

func TimeboostBridgeConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Uint16(prefix+".listen-port", DefaultTimeboostBridgeConfig.ListenPort, "timeboost inclusion listener listen port")
	f.Duration(prefix+".connection-timeout", DefaultTimeboostBridgeConfig.ConnectionTimeout, "timeboost inclusion list connection timeout")
	f.Int(prefix+".max-send-msg-size", DefaultTimeboostBridgeConfig.MaxSendMsgSize, "timeboost inclusion list send message size")
	f.Int(prefix+".max-receive-msg-size", DefaultTimeboostBridgeConfig.MaxReceiveMsgSize, "timeboost inclusion receive message size")
	f.Duration(prefix+".block-submit-timeout", DefaultTimeboostBridgeConfig.BlockSubmitTimeout, "sending block to timeboost connection timeout")
	f.String(prefix+".internal-timeboost-grpc-url", DefaultTimeboostBridgeConfig.InternalTimeboostGrpcUrl, "timeboost grpc server url")
}

func NewTimeboostBridge(config TimeboostBridgeConfig) (*TimeboostBridge, error) {
	return &TimeboostBridge{
		config:     config,
		grpcClient: nil,
	}, nil
}

type synchronizedTimeboostBlockQueue struct {
	queue []*protos.Block
	mutex sync.RWMutex
}

func (q *synchronizedTimeboostBlockQueue) enqueue(item *protos.Block) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.queue = append(q.queue, item)
}

func (q *synchronizedTimeboostBlockQueue) dequeue() {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if len(q.queue) > 0 {
		q.queue = q.queue[1:]
	}
}

func (q *synchronizedTimeboostBlockQueue) Peek() *protos.Block {
	q.mutex.RLock()
	defer q.mutex.RUnlock()
	if len(q.queue) == 0 {
		return nil
	}
	return q.queue[0]
}

type ForwardService struct {
	protos.UnimplementedForwardApiServer
	processInclusionList func(context.Context, *protos.InclusionList, *arbitrum_types.ConditionalOptions) error
}

// Implement the SubmitInclusionList RPC
func (s *ForwardService) SubmitInclusionList(ctx context.Context, req *protos.InclusionList) (*emptypb.Empty, error) {
	if err := s.processInclusionList(ctx, req, nil); err != nil {
		log.Error("failed to process inclusion list", "err", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// Send block to timeboost who will get certificate over the block hash and forward to hotshot
func (b *TimeboostBridge) EnqueueBlockToTimeboost(pos uint64, round uint64, encoded []byte) {
	protoBlock := &protos.Block{
		Number:  pos,
		Round:   round,
		Payload: encoded,
	}
	b.blockQueue.enqueue(protoBlock)
}

func (b *TimeboostBridge) blockSubmitter(timeout *time.Duration) time.Duration {
	block := b.blockQueue.Peek()
	if block != nil {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		if _, err := b.grpcClient.SubmitBlock(ctx, block); err != nil {
			log.Error("failed to submit block", "err", err)
			return 0
		}
		b.blockQueue.dequeue()
	}
	return 0
}

func (b *TimeboostBridge) Start(
	ctx context.Context,
	processInclusionList func(context.Context, *protos.InclusionList, *arbitrum_types.ConditionalOptions) error,
) error {
	if _, err := url.ParseRequestURI(b.config.InternalTimeboostGrpcUrl); err != nil {
		panic("timeboost grpc url must be a valid url")
	}
	oneMb := 1024 * 1024
	if b.config.MaxSendMsgSize < 5*oneMb || b.config.MaxSendMsgSize > 10*oneMb {
		panic("max send message size should be between 5 and 10 mb")
	}
	if b.config.MaxReceiveMsgSize < 5*oneMb || b.config.MaxReceiveMsgSize > 10*oneMb {
		panic("max receive message size should be bettern 5 and 10 mb")
	}
	if b.config.ConnectionTimeout < 3*time.Second || b.config.ConnectionTimeout > 10*time.Second {
		panic("connection timeout should be between 3 and 10 seconds")
	}

	b.StopWaiter.Start(ctx, b)

	// Grpc connection to timeboost for block submission
	grpcConn, err := grpc.NewClient(b.config.InternalTimeboostGrpcUrl, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("Failed to connect to gRPC server", "err", err)
		return err
	}
	log.Info("starting grpc client")
	b.grpcClient = protos.NewInternalApiClient(grpcConn)

	// Grpc server for inclusion list
	b.LaunchThread(func(ctx context.Context) {
		addr := fmt.Sprintf(":%d", b.config.ListenPort)
		lis, err := net.Listen("tcp", addr)
		if err != nil {
			panic(err)
		}
		server := grpc.NewServer(
			grpc.MaxRecvMsgSize(b.config.MaxSendMsgSize),
			grpc.MaxSendMsgSize(b.config.MaxSendMsgSize),
			grpc.ConnectionTimeout(b.config.ConnectionTimeout),
		)
		protos.RegisterForwardApiServer(server, &ForwardService{
			processInclusionList: processInclusionList,
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

	timeout := &b.config.BlockSubmitTimeout
	err = b.CallIterativelySafe(func(ctx context.Context) time.Duration {
		return b.blockSubmitter(timeout)
	})
	if err != nil {
		return err
	}
	return nil
}

func (l *TimeboostBridge) StopAndWait() {
	l.StopWaiter.StopAndWait()
}
