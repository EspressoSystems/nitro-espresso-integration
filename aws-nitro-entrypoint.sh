#!/bin/bash

# Adjust ownership for the .arbitrum folder to the 'user' inside the container
if [ -d "/home/user/.arbitrum" ]; then
   sudo chown -R user:user /home/user/.arbitrum
fi

# Start Nitro process
exec /usr/local/bin/nitro \
    --validation.wasm.enable-wasmroots-check=false \
    --conf.file /config/poster_config.json