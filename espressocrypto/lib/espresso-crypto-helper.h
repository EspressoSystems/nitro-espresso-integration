#ifndef ESPRESSO_CRYPTO_HELPER_H
#define ESPRESSO_CRYPTO_HELPER_H

#include "stdint.h"
#include <stdbool.h>
#include <stdlib.h>

bool verify_merkle_proof_helper(char *proof_c_bytes, char *header_c_bytes, char *block_comm_c_bytes, char *circuit_block_c_bytes);
bool verify_namespace_helper(uint64_t namespace, char *proof_c_bytes, char *commit_c_bytes, char *ns_table_c_bytes, char *tx_comm_c_bytes, char *common_data_c_bytes);

#endif // !ESPRESSO_CRYPTO_HELPER_H
