#!/usr/bin/env bash
set -euo pipefail

#enter the testnode directory
cd ../nitro-testnode

#Initialize a standard network not compatible with espresso
./test-node.bash --simple --init --detach 

#start espresso sequencer node
docker compose up espresso-dev-node --detach

#shutdown nitro node'
docker stop nitro-testnode-sequencer-1
# return to the migration directory to execute the script for starting an espresso-integrated nitro node
cd ../migration

#start nitro node in new docker container with espresso image (create a new script to do this from pieces of test-node.bash)
./create-espresso-integrated-nitro-node.bash 

#source env files needed by forge
source /test-env/.env

#forge script to deploy new OSP entry and upgrade actions
export NEW_OSP_ENTRY=`forge script --chain $CHAIN_NAME ../orbit-actions/contracts/parent-chain/contract-upgrades/DeployEspressoOsp.s.sol:DeployEspressoOsp --rpc-url $RPC_URL --broadcast --verify -vvvv | tail -n 1 | tr -d '\r\n'` # save ospentryaddr here to propegate to next part of script.    
#forge script to execute upgrade actions
forge script --chain $CHAIN_NAME ../orbit-actions/contracts/parent-chain/contract-upgrades/DeployAndExecuteEspressoMigrationActions.s.sol --rpc-url $RPC_URL --broadcast --verify -vvvv
#check the upgrade happened


#./test-node.bash --espresso --latest-espresso-image --validate --tokenbridge --init --detach
