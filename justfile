build:
    make build
    make build-replay-env

# force rebuild contracts and re-generate bindings
build-contracts:
    rm -f .make/solgen .make/solidity .make/espresso-gen
    make contracts

espresso-tests: build
    gotestsum --format standard-verbose --packages="\$packages" -- -v -timeout 15m -p 1 ./system_tests/... -run 'TestEspressoE2E'

tee-tests: build
    gotestsum --format standard-verbose --packages="\$packages" -- -v -timeout 15m -p 1 ./system_tests/... -run 'TestEspressoCaffNodeSnapshotTEE'
    gotestsum --format standard-verbose --packages="\$packages" -- -v -timeout 15m -p 1 ./system_tests/... -run 'TestEspressoCaffNodeRestartWithTeeType'

authdb-tests:
    gotestsum --format standard-verbose -- -v ./cmd/util/integrityattestation/... -run TestDeriveHmac
    rm -rf espresso/authdb/authdbancient
    gotestsum --format standard-verbose -- -v -timeout 15m -p 1 ./espresso/authdb
    rm -rf espresso/authdb/authdbancient
    rm -rf espresso/authdb/testancient
