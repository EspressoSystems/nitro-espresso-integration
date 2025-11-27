<br />
<p align="center">
  <a href="https://arbitrum.io/">
    <img src="https://arbitrum.io/assets/arbitrum/logo_color.png" alt="Logo" width="80" height="80">
  </a>

  <h3 align="center">Arbitrum Nitro - Espresso Integration</h3>

  <p align="center">
    <a href="https://developer.arbitrum.io/"><strong>Next Generation Ethereum L2 Technology »</strong></a>
    <br />
    <em>A fork of <a href="https://github.com/OffchainLabs/nitro">Arbitrum Nitro</a> managed by <a href="https://www.espressosys.com/">Espresso Systems</a></em>
  </p>
</p>

## About This Repository

This is a fork of [Arbitrum Nitro](https://github.com/OffchainLabs/nitro) maintained by Espresso Systems, extending the Nitro stack. For detailed integration documentation, see the [Espresso integration guides](https://docs.espressosys.com/network/guides/rollup-developers/nitro) and [Nitro Chain Integration](https://docs.espressosys.com/network/concepts/rollup-developers/integrating-an-optimistic-rollup/nitro).

## About Arbitrum Nitro

<img src="https://arbitrum.io/assets/arbitrum/logo_color.png" alt="Logo" width="80" height="80">

Nitro is the latest iteration of the Arbitrum technology. It is a fully integrated, complete
layer 2 optimistic rollup system, including fraud proofs, the sequencer, the token bridges,
advanced calldata compression, and more.

See the live docs-site [here](https://developer.arbitrum.io/) (or [here](https://github.com/OffchainLabs/arbitrum-docs) for markdown docs source.)

See [here](https://docs.arbitrum.io/audit-reports) for security audit reports.

The Nitro stack is built on several innovations. At its core is a new prover, which can do Arbitrum’s classic
interactive fraud proofs over WASM code. That means the L2 Arbitrum engine can be written and compiled using
standard languages and tools, replacing the custom-designed language and compiler used in previous Arbitrum
versions. In normal execution,
validators and nodes run the Nitro engine compiled to native code, switching to WASM if a fraud proof is needed.
We compile the core of Geth, the EVM engine that practically defines the Ethereum standard, right into Arbitrum.
So the previous custom-built EVM emulator is replaced by Geth, the most popular and well-supported Ethereum client.

The last piece of the stack is a slimmed-down version of our ArbOS component, rewritten in Go, which provides the
rest of what’s needed to run an L2 chain: things like cross-chain communication, and a new and improved batching
and compression system to minimize L1 costs.

Essentially, Nitro runs Geth at layer 2 on top of Ethereum, and can prove fraud over the core engine of Geth
compiled to WASM.

Arbitrum One successfully migrated from the Classic Arbitrum stack onto Nitro on 8/31/22. (See [state migration](https://developer.arbitrum.io/migration/state-migration) and [dapp migration](https://developer.arbitrum.io/migration/dapp_migration) for more info).

## Upstream Fork Management

### Active Branches

The repository currently maintains two active development branches:

- **integration**: Primary branch for general features and updates
- **celestia-integration**: Branch for Celestia DA integration

Legacy branches for previous versions (v3.5.6) are maintained separately:

- celestia-v3.5.6
- integration-v3.5.6

When Nitro v3.8.0 is merged into the main integration branch, v3.6.7 will be branched off as **integration-v3.6.7** for maintenance. Both integration and celestia-integration branches will continue to be maintained going forward.

### Forked Submodules

The following submodules have been forked for this integration:

- nitro-contracts
- bold
- testnode

Note: `go-ethereum` is used as an upstream dependency without modification.

## Running E2E Tests

### Prerequisites

- Nix package manager
- Docker daemon running

### Build Steps

0. Clone repository

```bash
git clone --recurse-submodules git@github.com:EspressoSystems/nitro-espresso-integration.git
```

1. For MacOS Users Only:

```bash
bash ./scripts/build-wasm-on-macos-with-nix
```

2. Enter development environment:

```bash
nix develop
```

3. Build environment:

```bash
make build
make build-replay-env
```

4. Run E2E tests (ensure Docker is running):

**Option A: Clean output**

```bash
go test -v -timeout 60m -p 1 ./system_tests/... -run 'TestEspressoE2E' 2>&1 |  sed '/ld: warning/d; /object file/d; /^$/d' | tee test_output.log
```

This filters out Rust linker warnings in real-time, showing only test output and errors.

**Option B: Full output (for debugging)**

```bash
gotestsum --format=testname --packages="./system_tests/..." -- -v -timeout 15m -p 1 -count=1 -run 'TestEspressoE2E'
```

Shows all output including linker warnings.

Alternatively to steps 3 and 4 you can run:

```bash
just espresso-tests
```

Note: The E2E tests typically take around 10-15 minutes to complete.

## License

Nitro is currently licensed under a [Business Source License](./LICENSE.md), similar to our friends at Uniswap and Aave, with an "Additional Use Grant" to ensure that everyone can have full comfort using and running nodes on all public Arbitrum chains.

The Additional Use Grant also permits the deployment of the Nitro software, in a permissionless fashion and without cost, as a new blockchain provided that the chain settles to either Arbitrum One or Arbitrum Nova.

For those that prefer to deploy the Nitro software either directly on Ethereum (i.e. an L2) or have it settle to another Layer-2 on top of Ethereum, the [Arbitrum Expansion Program (the "AEP")](https://docs.arbitrum.foundation/aep/ArbitrumExpansionProgramTerms.pdf) was recently established. The AEP allows for the permissionless deployment in the aforementioned fashion provided that 10% of net revenue (as more fully described in the AEP) is contributed back to the Arbitrum community in accordance with the requirements of the AEP.

## Contact

Discord - [Arbitrum](https://discord.com/invite/5KE54JwyTs)

Twitter: [Arbitrum](https://twitter.com/arbitrum)
