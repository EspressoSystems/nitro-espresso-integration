build:
    make build
    make build-replay-env

espresso-tests: build
    gotestsum --format standard-verbose --packages="\$packages" -- -v -timeout 15m -p 1 ./system_tests/... -run 'TestEspressoE2E'

tee-tests: build
    gotestsum --format standard-verbose --packages="\$packages" -- -v -timeout 15m -p 1 ./system_tests/... -run 'TestEspressoCaffNodeRestartWithTeeType'

authdb-tests:
    rm -rf espresso/authdb/authdbancient
    gotestsum --format standard-verbose -- -v -timeout 15m -p 1 ./espresso/authdb
    rm -rf espresso/authdb/authdbancient
