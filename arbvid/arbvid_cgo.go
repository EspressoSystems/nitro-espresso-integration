//go:build !js
// +build !js

package arbvid

import espressoTypes "github.com/EspressoSystems/espresso-sequencer-go/types"

// This is where we would use cgo to call Rust code to verify a namespace using the C FFI
// TODO stretch goal: https://github.com/EspressoSystems/nitro-espresso-integration/issues/71
func verifyNamespace(namespace uint64, proof espressoTypes.NmtProof, block_comm espressoTypes.TaggedBase64, ns_table espressoTypes.NsTable, txs []espressoTypes.Bytes) error {
}
