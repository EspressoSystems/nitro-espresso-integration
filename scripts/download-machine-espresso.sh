#!/usr/bin/env bash
# Same as ./download-machine.sh but for the espresso integration.
#
# The url_base has been changed to point to the espresso integration repo such
# that it downloads the replay wasm binary for the integration instead.
set -e

mkdir "$2"
ln -sfT "$2" latest
cd "$2"
echo "$2" > module-root.txt
url_base="https://github.com/EspressoSystems/nitro-espresso-integrations/releases/download/$1"
wget "$url_base/machine.wavm.br"

status_code="$(curl -LI "$url_base/replay.wasm" -so /dev/null -w '%{http_code}')"
if [ "$status_code" -ne 404 ]; then
	wget "$url_base/replay.wasm"
fi
