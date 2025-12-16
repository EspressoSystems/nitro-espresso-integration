package gethexec

import (
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
)

// Publish following functions for espresso caff node to access the blockchain
func (s *ExecutionEngine) Bc() *core.BlockChain {
	return s.bc
}
func (s *ExecutionEngine) AppendBlock(block *types.Block, statedb *state.StateDB, receipts types.Receipts, duration time.Duration) error {
	return s.appendBlock(block, statedb, receipts, duration)
}
