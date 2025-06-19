# Espresso Sequencer Go SDK

This package provides tools and interfaces for working with the
[Espresso sequencer](https://github.com/EspressoSystems/espresso-network) in Go. It should
(eventually) provide everything needed to integrate a rollup written in Go with the Espresso
sequencer.

## Development

- Obtain code:

        git clone git@github.com:EspressoSystems/espresso-network-go
        git submodule update --init --recursive

- Make sure [nix](https://nixos.org/download.html) is installed.
- Activate the environment with `nix-shell`, or `nix develop`, or `direnv allow` if using [direnv](https://direnv.net/).

## Verification

Run the following command to download the static library for the current platform.

    sudo go run github.com/EspressoSystems/espresso-network-go/download download

Or you can specify the version with `-v` flag.

    sudo go run github.com/EspressoSystems/espresso-network-go/download download -v 0.0.32

Build the verification library.

    go build github.com/EspressoSystems/espresso-network-go/verification

You can also clean the downloaded files with the following command.

    sudo go run github.com/EspressoSystems/espresso-network-go/download clean

## Run the linter and unit tests

    just lint
    just test

## Generating contract bindings

    just bind-light-client

## Generating verification test data

For `TestVerifyNamespaceWithRealData` test, you can get the data as follows:
To get the `transaction_in_block` test data, run the following in query-service:
`https://query.decaf.testnet.espresso.network/v1/availability/block/block-number/namespace/namespace-id`

You can get `vid_common` test data by running the following in query-service:
`https://query.decaf.testnet.espresso.network/v1/availability/vid/common/block-height`

Finally, you can get `header` test data by running the following in query-service:
`https://query.decaf.testnet.espresso.network/v1/availability/header/block-height`

For `TestNamespaceProofVerification` to generate `namespace_proof_test_data.json`, you can run the following command:

You need to generate the data using `espresso-network` repo and add println statements similar to the following [code](https://github.com/EspressoSystems/espresso-network/blob/generate-verfification-data/types/src/v0/impls/block/full_payload/ns_proof/avidm.rs#L162-L170)

To run the test, you can run the following command:

```
cd espresso-network/types
cargo test ns_proof -- --nocapture
```

## Generating merkle proof test data

To generate the merkle proof test data, you first need to start the dev node with anvil node:

```
cd client/dev-node
docker compose up
```

Then to generate the merkle proof test data, uncomment the `TestGenerateMerkleProofTestData` test in `verification/merkle_proof_test_data_generation_test.go` which generates the merkle proof test data json file

Then run the test using:

```
go test ./verification -run ^TestGenerateMerkleProofTestData$
```

Use dev node and visit http://localhost:{port}/v0/api/dev-info to get the light client address
