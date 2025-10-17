# CaffNode Authenticated Storage Developer Notes

## Relevant control flow in CaffNode

Main entry: `cmd/nitro/nitro.go::mainImpl()`
- `integrityattestation.GenerateHMAC()`: prepare HMAC function with HMAC key derived from ECDSA key in enclave.
- `chainDb, l2BlockChain, err := openInitializeChainDb(.., nodeConfig, l1Client, ..)`: 
  - `chainData, err := Node.OpenDatabaseWithFreezerWithExtraOptions("l2chaindata", ..)` where `chainData ethdb.Database` is the Pebble/LevelDB handle with dir and ancient dir mounted.
  - `chainDb := rawdb.WrapDatabaseWithWasm(chainData, wasmDb, ..)` further wraps plain chaindata, resulting `chainDb ethdb.Database`.
  - `l2BlockChain, err := gethexec.GetBlockChain(chainDb,.. chainConfig, ..)` where resulting `l2BlockChain core.Blockchain`. Inside, invoking `core/blockchain.go::NewBlockchain()`: 
    - `!bc.HasState(bc.CurrentBlock())`: if don't have head state, prepare a recovery block height depending on whether the state root stored under `SnapshotRootKey` is available (if not, won't enter recover mode, `snapconfig.Recovery = false`).
    - `bc.statedb` will be reinitialized to snapshot (fetch from db or reconstruct from scratch in a background thread)
