package decentralized_timeboost

import (
	"context"

	// Protobuf imports for grpc calls
	protos "github.com/EspressoSystems/timeboost-proto/go-generated"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/log"
)

type ForwardService struct {
	protos.UnimplementedForwardApiServer
	ProcessInclusionList func(context.Context, *protos.InclusionList, *arbitrum_types.ConditionalOptions) error
}

// Implement the SubmitInclusionList RPC
func (s *ForwardService) SubmitInclusionList(ctx context.Context, req *protos.InclusionList) (*emptypb.Empty, error) {
	if err := s.ProcessInclusionList(ctx, req, nil); err != nil {
		log.Error("failed to process inclusion list", "err", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
