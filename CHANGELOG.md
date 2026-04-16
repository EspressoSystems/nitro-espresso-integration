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

## 1.0.0 (2026-04-16)


### Features

* **nitro-val:** implement ValidationNodeConfig.Validate with logging and persistent checks ([#3735](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3735)) ([c46e1e9](https://github.com/EspressoSystems/nitro-espresso-integration/commit/c46e1e91b1aaf75b8465313deb354cc401ecee03))
* Upgrade to v3.9.8 ([#1030](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1030)) ([249a357](https://github.com/EspressoSystems/nitro-espresso-integration/commit/249a3572d349d144e70e1226523f6c76622f76de))


### Bug Fixes

* correct error aggregation for multi-target compile results ([1403b13](https://github.com/EspressoSystems/nitro-espresso-integration/commit/1403b13faf0cc9bd4a527f55a85d471148725877))
* correct safe-wait delta calculation in edge tracker and transact ([#3633](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3633)) ([8cbcf80](https://github.com/EspressoSystems/nitro-espresso-integration/commit/8cbcf8015d5d55d86b4c77b6dcb31f5d87c78a56))
* Correctly count struct fields in the structinit linter ([#3579](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3579)) ([cb86fca](https://github.com/EspressoSystems/nitro-espresso-integration/commit/cb86fcaa6972ce2a9680762721ca489bcd8068c5))
* Docker and tool version checks for accurate reporting ([40552d5](https://github.com/EspressoSystems/nitro-espresso-integration/commit/40552d533b3e9b49e64da0614ee0c389eb867a6c))
* Docker and tool version checks for accurate reporting ([a9e7e07](https://github.com/EspressoSystems/nitro-espresso-integration/commit/a9e7e07061e82e926f41dd2626d51ed2780fa86d))
* Enable EIP7883 and EIP7823 for Arbos50 ([#3807](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3807)) ([9270a05](https://github.com/EspressoSystems/nitro-espresso-integration/commit/9270a057fa8b30fb960872d74f7632aae8f5efbe))
* enable ReadHeaderTimeout config and add tests ([#3683](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3683)) ([a4da683](https://github.com/EspressoSystems/nitro-espresso-integration/commit/a4da68356b16a049551e0a9313db5d0c20e64080))
* espresso caff node test ([#1035](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1035)) ([cf2530f](https://github.com/EspressoSystems/nitro-espresso-integration/commit/cf2530ffe0754598621942d18f7195fdda73911c))
* **events:** use stable subscription ids and remove by id ([#3563](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3563)) ([b580dd7](https://github.com/EspressoSystems/nitro-espresso-integration/commit/b580dd7c4026dac84e8dbcbe5aa3998b59d7e9ea))
* extract SerializedChainConfig from json directly ([bb7ffc1](https://github.com/EspressoSystems/nitro-espresso-integration/commit/bb7ffc1302cb0dabdbb3dafedb4d0bfebb011f1c))
* fix nonce validation ([#1021](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1021)) ([#1031](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1031)) ([a97127e](https://github.com/EspressoSystems/nitro-espresso-integration/commit/a97127eb47530165059e1a0f44bd7b7895a1f69c))
* **fuzz:** validate --fuzzcache-path against fuzzcachepath, not binpath ([#3656](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3656)) ([b6bad4a](https://github.com/EspressoSystems/nitro-espresso-integration/commit/b6bad4a9d4b5582c85cc103eeae85faddcc2ebec))
* Make structinit linter work cross packages ([#3586](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3586)) ([6d29e7a](https://github.com/EspressoSystems/nitro-espresso-integration/commit/6d29e7ae664b31d5fc574d16435cf74cfc381cf4))
* missing reassignment of rivals when recording creation times in block snapshot ([#3619](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3619)) ([a964782](https://github.com/EspressoSystems/nitro-espresso-integration/commit/a9647825cc1b260ba132efc5e6a7cee3595a77ba))
* recreate-missing-state-from panic ([ac3112a](https://github.com/EspressoSystems/nitro-espresso-integration/commit/ac3112a6cd9714da5af48dfcda9f34787491abae))
* release pipeline for v3.9.8 ([#1036](https://github.com/EspressoSystems/nitro-espresso-integration/issues/1036)) ([400c412](https://github.com/EspressoSystems/nitro-espresso-integration/commit/400c4129c284e55728e6e2404fe0258b26f75dda))
* script behavior for safer execution and error handling ([#3823](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3823)) ([152aec9](https://github.com/EspressoSystems/nitro-espresso-integration/commit/152aec9e4ee285680afddccd34cf5f2563657eec))
* **solimpl:** correct UpperChild zero-check to use UpperChildId ([#3703](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3703)) ([26f406c](https://github.com/EspressoSystems/nitro-espresso-integration/commit/26f406cc4ffa5e2e75cf32d1af53cc12f154d834))
* use Counter for nonceFailureCache overflow metric ([#3648](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3648)) ([cf87312](https://github.com/EspressoSystems/nitro-espresso-integration/commit/cf87312f3dccdf0ec22de427c3b9173f80a4d03f))
* use RLock instead of Lock in CollectMachineHashes for concurrent access ([2f3201a](https://github.com/EspressoSystems/nitro-espresso-integration/commit/2f3201aa6a2cfef1b8f19c6d4b2a222e73b5da12))
* use RLock instead of Lock in CollectMachineHashes for concurrent… ([4363545](https://github.com/EspressoSystems/nitro-espresso-integration/commit/4363545646d76f27fa928415fdcbe6bdc14a2e15))


### Reverts

* 3700 ([#3711](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3711)) ([cb742ea](https://github.com/EspressoSystems/nitro-espresso-integration/commit/cb742ea9c2fce0d9edbd508f6ecf45ee9dfed11e))


### Documentation

* fix inverted comments in BoldMachine methods ([#3980](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3980)) ([8da9e1e](https://github.com/EspressoSystems/nitro-espresso-integration/commit/8da9e1ece29077fb93e1768db89b98f100b38e05))
* update broken link ([#3942](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3942)) ([a4f103f](https://github.com/EspressoSystems/nitro-espresso-integration/commit/a4f103fd77b2f71ad6c83930c5b582547aaba75e))
* update broken link ([#3968](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3968)) ([ef01ed5](https://github.com/EspressoSystems/nitro-espresso-integration/commit/ef01ed507598f4d2ba45f05d00c8157efd017e7b))

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
