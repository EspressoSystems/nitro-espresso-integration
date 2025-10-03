#!/bin/bash -e
# Start vsock proxies
# Outbound enclave connections

# AWS KMS
socat VSOCK-LISTEN:8002,fork,keepalive TCP:kms.$AWS_REGION.amazonaws.com:443,keepalive &
# AWS STS
socat VSOCK-LISTEN:8003,fork,keepalive TCP:sts.$AWS_REGION.amazonaws.com:443,keepalive &
# L1 NODE
socat VSOCK-LISTEN:8004,fork,keepalive TCP:$L1_NODE,keepalive &
# L1 BEACON NODE
socat VSOCK-LISTEN:8005,fork,keepalive TCP:$L1_BEACON_NODE,keepalive &
# NFS Server
socat VSOCK-LISTEN:20000,fork,keepalive TCP:$NFS_SERVER,keepalive &
# Decaf query node 1
socat VSOCK-LISTEN:8006,fork,keepalive TCP:query-0.decaf.testnet.espresso.network:443,keepalive &
# Decaf query node 2
socat VSOCK-LISTEN:8007,fork,keepalive TCP:query-1.decaf.testnet.espresso.network:443,keepalive &
# Decaf query node 3
socat VSOCK-LISTEN:8008,fork,keepalive TCP:query-2.decaf.testnet.espresso.network:443,keepalive &
# Decaf query node 4
socat VSOCK-LISTEN:8009,fork,keepalive TCP:query-3.decaf.testnet.espresso.network:443,keepalive &


# Inbound enclave connections

# Supervisor API
socat TCP-LISTEN:9001,fork,reuseaddr,keepalive,bind=127.0.0.1 VSOCK-CONNECT:$ENCLAVE_CID:9001,keepalive &
# RPC HTTP
socat TCP-LISTEN:$RPC_HTTP_PORT,fork,reuseaddr,keepalive VSOCK-CONNECT:$ENCLAVE_CID:10000,keepalive &
# RPC WS
socat TCP-LISTEN:$RPC_WS_PORT,fork,reuseaddr,keepalive VSOCK-CONNECT:$ENCLAVE_CID:10001,keepalive &

nitro-cli run-enclave --debug-mode --eif-path /home/nitro.eif --enclave-cid $ENCLAVE_CID --cpu-count $CPU_COUNT --memory $MEMORY_MIB $EXTRA_OPTIONS
enclave_id=$(nitro-cli describe-enclaves | jq -r ".[0].EnclaveID")
echo "-------------------------------"
echo "Enclave ID is $enclave_id"
echo "-------------------------------"

nitro-cli console --enclave-id $enclave_id || true && tail -f /dev/null
