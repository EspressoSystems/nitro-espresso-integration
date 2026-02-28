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
