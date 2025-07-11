package gethexec

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	flag "github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	// Protobuf imports for grpc calls
	gethexec "github.com/offchainlabs/nitro/execution/gethexec/protos"
	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type ForwardService struct {
	gethexec.UnimplementedForwardApiServer
	processInclusionList func(context.Context, *gethexec.InclusionList, *arbitrum_types.ConditionalOptions) error
}

// Implement the SubmitInclusionList RPC
func (s *ForwardService) SubmitInclusionList(ctx context.Context, req *gethexec.InclusionList) (*emptypb.Empty, error) {
	if err := s.processInclusionList(ctx, req, nil); err != nil {
		log.Error("failed to process inclusion list", "err", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

type TimeboostBridge struct {
	stopwaiter.StopWaiter
	config     TimeboostBridgeConfig
	grpcClient gethexec.InternalApiClient
}

type TimeboostBridgeConfig struct {
	ListenPort               uint16        `koanf:"listen-port"`
	ConnectionTimeout        time.Duration `koanf:"connection-timeout"`
	MaxSendMsgSize           int           `koanf:"max-send-msg-size"`
	MaxReceiveMsgSize        int           `koanf:"max-receive-msg-size"`
	InternalTimeboostGrpcUrl string        `koanf:"internal-timeboost-grpc-url"`
}

var DefaultTimeboostBridgeConfig = TimeboostBridgeConfig{
	ListenPort:               55000,            // Default listen port that timeboost will try and connect to
	ConnectionTimeout:        5 * time.Second,  // Max time for grpc connection timeboost
	MaxSendMsgSize:           5 * 1024 * 1024,  // Max msg receive size from timeboost
	MaxReceiveMsgSize:        5 * 1024 * 1024,  // Max msg send size to timeboost
	InternalTimeboostGrpcUrl: "localhost:5000", // Timeboost grpc server url
}

func TimeboostBridgeConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Uint16(prefix+".listen-port", DefaultTimeboostBridgeConfig.ListenPort, "timeboost inclusion listener listen port")
	f.Duration(prefix+".connection-timeout", DefaultTimeboostBridgeConfig.ConnectionTimeout, "timeboost inclusion list connection timeout")
	f.Int(prefix+".max-send-msg-size", DefaultTimeboostBridgeConfig.MaxSendMsgSize, "timeboost inclusion list send message size")
	f.Int(prefix+".max-receive-msg-size", DefaultTimeboostBridgeConfig.MaxReceiveMsgSize, "timeboost inclusion receive message size")
	f.String(prefix+".internal-timeboost-grpc-url", DefaultTimeboostBridgeConfig.InternalTimeboostGrpcUrl, "timeboost grpc server url")
}

func NewTimeboostBridge(config TimeboostBridgeConfig) (*TimeboostBridge, error) {
	return &TimeboostBridge{
		config:     config,
		grpcClient: nil,
	}, nil
}

// Send block to timeboost who will get certificate over the block hash and forward to hotshot
func (l *TimeboostBridge) SendBlockToTimeboost(block *types.Block, round uint64, chainId uint32) error {
	txns, err := rlp.EncodeToBytes(block.Transactions())
	if err != nil {
		return err
	}
	protoBlock := &gethexec.Block{
		Namespace: chainId,
		Round:     round,
		Hash:      block.Hash().Bytes(),
		// TODO: Proper hotshot payload
		Payload: txns,
	}
	ctx := context.Background()
	if _, err := l.grpcClient.SubmitBlock(ctx, protoBlock); err != nil {
		log.Error("failed to submit block", "err", err)
		return err
	}
	return nil
}

func (l *TimeboostBridge) Start(
	ctx context.Context,
	processInclusionList func(context.Context, *gethexec.InclusionList, *arbitrum_types.ConditionalOptions) error,
) error {
	if _, err := url.ParseRequestURI(l.config.InternalTimeboostGrpcUrl); err != nil {
		panic("timeboost grpc url must be a valid url")
	}

	l.StopWaiter.Start(ctx, l)

	// Grpc connection to timeboost for block submission
	grpcConn, err := grpc.NewClient(l.config.InternalTimeboostGrpcUrl, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("Failed to connect to gRPC server", "err", err)
		return err
	}
	l.grpcClient = gethexec.NewInternalApiClient(grpcConn)

	// Grpc server for inclusion list
	l.LaunchThread(func(ctx context.Context) {
		addr := fmt.Sprintf(":%d", l.config.ListenPort)
		lis, err := net.Listen("tcp", addr)
		if err != nil {
			panic(err)
		}
		server := grpc.NewServer(grpc.MaxRecvMsgSize(l.config.MaxSendMsgSize), grpc.MaxSendMsgSize(l.config.MaxSendMsgSize), grpc.ConnectionTimeout(l.config.ConnectionTimeout))
		gethexec.RegisterForwardApiServer(server, &ForwardService{
			processInclusionList: processInclusionList,
		})
		go func() {
			<-ctx.Done()
			log.Info("Shutting down gRPC server...")
			server.GracefulStop()
		}()
		err = server.Serve(lis)
		if err != nil {
			panic(err)
		}
	})
	return nil
}

func (l *TimeboostBridge) StopAndWait() {
	l.StopWaiter.StopAndWait()
}
