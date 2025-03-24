#!/bin/bash

set -e

echo "Up loopback interface"
ip link set lo up || true
sleep 2

echo "Ensure loopback addresses exist"
if ! ip addr show dev lo | grep -q "127.0.0.200"; then
  ip addr add 127.0.0.200/32 dev lo:0
  ip link set dev lo:0 up
fi
sleep 2

echo "Start vsock proxy"
socat TCP-LISTEN:2049,bind=127.0.0.200,fork,reuseaddr,keepalive VSOCK-CONNECT:3:8004,keepalive &
sleep 2

echo "Mount NFS"
mount -t nfs4 127.0.0.200:/ /home/user/.arbitrum

# Start Nitro process
exec gosu enclave-user:enclave-user /usr/local/bin/nitro \
    --validation.wasm.enable-wasmroots-check=false \
    --conf.file /config/poster_config.json