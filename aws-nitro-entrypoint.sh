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

PORT=8005
echo "Starting tcp listener on port 8005 for INT signal"
start_vsock_termination_server() {
    socat VSOCK-LISTEN:$PORT,fork,keepalive SYSTEM:'
        while read -r message; do
            echo "Received shutdown"
            pkill -INT -f "/usr/local/bin/nitro"
            break
        done
    '
}

start_vsock_termination_server &

# Start Nitro process
exec /usr/local/bin/nitro \
  --validation.wasm.enable-wasmroots-check=false \
  --conf.file /config/poster_config.json 