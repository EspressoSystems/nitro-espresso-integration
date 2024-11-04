
package espressocrypto

/*
#cgo LDFLAGS: ./lib/libespresso_crypto_helper.a -ldl
#include "./lib/espresso-crypto-helper.h"
#include <stdlib.h>
*/
import "C"

func verifyNamespace(namespace uint64, proof []byte, block_comm []byte, ns_table []byte, tx_comm []byte, common_data []byte) bool {
  c_namespace := C.uint64_t(namespace)
  proof_string := string(proof)
  block_comm_string := string(proof)
  ns_table_string := string(proof)
  tx_comm_string := string(proof)
  common_data_string := string(proof) 

  //We allocate many c pointers here, we must defer freeing them all to the end of the function.
  c_proof_string := C.CString(proof_string)
  c_block_comm_string := C.CString(block_comm_string)
  c_ns_table_string := C.CString(ns_table_string)
  c_tx_comm_string := C.CString(tx_comm_string)
  c_common_data_string := C.CString(common_data_string)


  valid_namespace_proof := bool(C.verify_namespace_helper(c_namespace, c_proof_string, c_block_comm_string, c_ns_table_string, c_tx_comm_string, c_common_data_string))
  return valid_namespace_proof
}

func verifyMerkleProof(proof []byte, header []byte, block_comm []byte, circuit_comm_bytes []byte) bool {
  proof_string := string(proof) 
  header_string := string(header)
  block_comm_string := string(block_comm)
  circuit_comm_bytes_string := string(circuit_comm_bytes)

  c_proof_string := C.CString(proof_string)
  c_header_string := C.CString(header_string)
  c_block_comm_string := C.CString(block_comm_string)
  c_circuit_comm_bytes_string := C.CString(circuit_comm_bytes_string)

  
  valid_merkle_proof := bool(C.verify_merkle_proof_helper(c_proof_string, c_header_string, c_block_comm_string, c_circuit_comm_bytes_string))

  return valid_merkle_proof

}
