# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Versioning Scheme

This project uses compound versioning: `v{upstream}-espresso-v{espresso}`

- **Upstream version**: Tracks the base Nitro version from upstream
- **Espresso version**: Tracks Espresso-specific changes (auto-incremented)

Example: `v3.9.2-espresso-v0.1.0`

---

## [1.1.0](https://github.com/EspressoSystems/nitro-espresso-integration/compare/v3.9.2-espresso-v1.0.1...v3.9.2-espresso-v1.1.0) (2026-04-06)


### Features

* Add code path for SGX private key ([#1000](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1000)) ([bc99913](https://github.com/EspressoSystems/nitro-espresso-integration/commit/bc99913658e87e8e44af7328bf2b1448000ad6ed))
* Streamer to filter duplicated messages on insert ([#999](https://github.com/EspressoSystems/nitro-espresso-integration/issues/999)) ([37d59c4](https://github.com/EspressoSystems/nitro-espresso-integration/commit/37d59c48991e146d48fb78374e9eef0dc51b4e91))
* Support eip 712 ([#1008](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1008)) ([5eb9ca8](https://github.com/EspressoSystems/nitro-espresso-integration/commit/5eb9ca8cb56b28e31e92c92170dbb5c055f4feed))


### Bug Fixes

* add underflow guard in delayed message fetcher ([#1009](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1009)) ([3697f1d](https://github.com/EspressoSystems/nitro-espresso-integration/commit/3697f1d57251193c1fc203d4c64fd015e8b268bf))
* batch poster test ([#1029](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1029)) ([f7dd2bb](https://github.com/EspressoSystems/nitro-espresso-integration/commit/f7dd2bb0318e8b29362ae0ab0d0fdccbda750dcc))
* fatal err on nonce mismatch ([#1016](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1016)) ([9774b93](https://github.com/EspressoSystems/nitro-espresso-integration/commit/9774b93a49b3562c3de88bf8488672bd5e2d1c47))
* fix nonce validation ([#1021](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1021)) ([c9b9549](https://github.com/EspressoSystems/nitro-espresso-integration/commit/c9b9549760dd2b762c76bdb6be11623a10aa02e0))
* rm use of EspressoRollupSequencerManager ([#1015](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1015)) ([110a577](https://github.com/EspressoSystems/nitro-espresso-integration/commit/110a577321bd59334b591330be2d649bac65013f))
* stop parsing if sig verification fails ([#1010](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1010)) ([28bce47](https://github.com/EspressoSystems/nitro-espresso-integration/commit/28bce47a436b64ba44f6b3162f01f5d8b1db7279))

## [1.0.1](https://github.com/EspressoSystems/nitro-espresso-integration/compare/v3.9.2-espresso-v1.0.0...v3.9.2-espresso-v1.0.1) (2026-02-09)


### Bug Fixes

* run ci on v3.9.2 branches and add githook for checking commit messages ([#987](https://github.com/EspressoSystems/nitro-espresso-integration/issues/987)) ([ec638bd](https://github.com/EspressoSystems/nitro-espresso-integration/commit/ec638bdd2158fe91dfecfda3e1995f01d6349814))

## 1.0.0 (2026-02-06)

### Features
* Adds support for Attestation service #879 
* Adds support for Minimum Hotshot block number #895 
* Batcher Addr monitor refactor #881
* Add limit to registering the signer #931 
* Add support for websocket connection in TEE #939 
* feat: move signer registration loop to the verifier #958
* Upgrade to v3.9.2 (#873)
* Add attestation service ZK (#879)
* Minimum hotshot block number in espresso streamer (#895) (#897)

### Bug Fixes
* Fixes Regression tests #877 
* Solve the latestBaseFee taking a Nil value #878
* Removes light client address from the config #900 
* Removes light client address from streamer #907 
* Add namespace range endpoint #901 
* Remove max base fee while registering signer #921 
* Abstract Espresso config as a type #916 
* Fix config bug #933 
* Fix throughput tests #930 
* Fix from block remapping #949 
* Cleanup Espresso config #932 
* Add missing hotshot-url remapping #967
* Fix typo and default value in the config #973
* Add missing hotshot-url remapping (#967)
* move signer registration loop to the verifier (#958)
* Cleanup Espresso Config (#932)
* fix: from-block remapping (#949)
* optimize config migration script (#936)
* fix websocket connection in AWS Nitro (#939)
* Solve the latestBaseFee taking a Nil value (#878) (#889)
* remove light client address (#900)
* Refactor batcher address monitor (#881)
* rm light client reader from polling esp streamer (#907)
* Add Namespace Range endpoint to streamer (#901) (#909)
* Remove Max Base Fee upon Registering Signer #921
* abstract espresso configs as type (#916)
* Fix config (#933)
* Fix throughput test (#930)
* Registering the signer should have a limit (#931)

---

<!-- Release-Please will add new entries above this line -->
