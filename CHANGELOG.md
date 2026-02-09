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

* move signer registration loop to the verifier ([#958](https://github.com/EspressoSystems/nitro-espresso-integration/issues/958)) ([87c9b6a](https://github.com/EspressoSystems/nitro-espresso-integration/commit/87c9b6a61390b1953b9c1f3caa0d65e6ed9b061d))
* **nitro-val:** implement ValidationNodeConfig.Validate with logging and persistent checks ([#3735](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3735)) ([c46e1e9](https://github.com/EspressoSystems/nitro-espresso-integration/commit/c46e1e91b1aaf75b8465313deb354cc401ecee03))


### Bug Fixes

* **backend:** fix error handling and add comprehensive tests ([#3510](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3510)) ([4564905](https://github.com/EspressoSystems/nitro-espresso-integration/commit/456490506395dd886b3df0457ee6be2127daac5a))
* correct error aggregation for multi-target compile results ([1403b13](https://github.com/EspressoSystems/nitro-espresso-integration/commit/1403b13faf0cc9bd4a527f55a85d471148725877))
* correct error aggregation for multi-target compile results ([4f0b913](https://github.com/EspressoSystems/nitro-espresso-integration/commit/4f0b913fe79c40278d8a05befb5eb52235785f52))
* correct safe-wait delta calculation in edge tracker and transact ([#3633](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3633)) ([8cbcf80](https://github.com/EspressoSystems/nitro-espresso-integration/commit/8cbcf8015d5d55d86b4c77b6dcb31f5d87c78a56))
* Correctly count struct fields in the structinit linter ([#3579](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3579)) ([cb86fca](https://github.com/EspressoSystems/nitro-espresso-integration/commit/cb86fcaa6972ce2a9680762721ca489bcd8068c5))
* Docker and tool version checks for accurate reporting ([40552d5](https://github.com/EspressoSystems/nitro-espresso-integration/commit/40552d533b3e9b49e64da0614ee0c389eb867a6c))
* Docker and tool version checks for accurate reporting ([a9e7e07](https://github.com/EspressoSystems/nitro-espresso-integration/commit/a9e7e07061e82e926f41dd2626d51ed2780fa86d))
* Enable EIP7883 and EIP7823 for Arbos50 ([#3807](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3807)) ([9270a05](https://github.com/EspressoSystems/nitro-espresso-integration/commit/9270a057fa8b30fb960872d74f7632aae8f5efbe))
* enable ReadHeaderTimeout config and add tests ([#3683](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3683)) ([a4da683](https://github.com/EspressoSystems/nitro-espresso-integration/commit/a4da68356b16a049551e0a9313db5d0c20e64080))
* **events:** use stable subscription ids and remove by id ([#3563](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3563)) ([b580dd7](https://github.com/EspressoSystems/nitro-espresso-integration/commit/b580dd7c4026dac84e8dbcbe5aa3998b59d7e9ea))
* extract SerializedChainConfig from json directly ([bb7ffc1](https://github.com/EspressoSystems/nitro-espresso-integration/commit/bb7ffc1302cb0dabdbb3dafedb4d0bfebb011f1c))
* from-block remapping ([#949](https://github.com/EspressoSystems/nitro-espresso-integration/issues/949)) ([ed6ef14](https://github.com/EspressoSystems/nitro-espresso-integration/commit/ed6ef14c6b2034952c669ef80cf97f48ffe86153))
* **fuzz:** validate --fuzzcache-path against fuzzcachepath, not binpath ([#3656](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3656)) ([b6bad4a](https://github.com/EspressoSystems/nitro-espresso-integration/commit/b6bad4a9d4b5582c85cc103eeae85faddcc2ebec))
* Make structinit linter work cross packages ([#3586](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3586)) ([6d29e7a](https://github.com/EspressoSystems/nitro-espresso-integration/commit/6d29e7ae664b31d5fc574d16435cf74cfc381cf4))
* missing reassignment of rivals when recording creation times in block snapshot ([#3619](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3619)) ([a964782](https://github.com/EspressoSystems/nitro-espresso-integration/commit/a9647825cc1b260ba132efc5e6a7cee3595a77ba))
* recreate-missing-state-from panic ([ac3112a](https://github.com/EspressoSystems/nitro-espresso-integration/commit/ac3112a6cd9714da5af48dfcda9f34787491abae))
* regression ci to use v3.9.2 integration branch ([#978](https://github.com/EspressoSystems/nitro-espresso-integration/issues/978)) ([989b66a](https://github.com/EspressoSystems/nitro-espresso-integration/commit/989b66a57282dadbcfda0c585ba84cad75219ebb))
* release pipeline for v3.9.2 ([#972](https://github.com/EspressoSystems/nitro-espresso-integration/issues/972)) ([42960f3](https://github.com/EspressoSystems/nitro-espresso-integration/commit/42960f371c2458bdf0bd30f2be80353fb9c9da86))
* resolve race condition in storage test goroutines ([#3509](https://github.com/EspressoSystems/nitro-espresso-integration/issues/3509)) ([e6d71a3](https://github.com/EspressoSystems/nitro-espresso-integration/commit/e6d71a363af9ae6130621b189860f8d2b60ca6ec))
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

## [0.0.1] - 2026-02-04

### Features

* Initial release with automated changelog and release pipeline

---

<!-- Release-Please will add new entries above this line -->
