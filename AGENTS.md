# Project overview
This project is a clone of offchain's labs [nitro](https://github.com/OffchainLabs/nitro/) Branches in our repository are typically forked from specific upstream Nitro versions. For example, a branch named integration-v3.9.2 indicates that it was forked from Nitro version v3.9.2. Our integration integrates nitro's code with [Espresso Network ](https://docs.espressosys.com/) to help nitro chains achieve fast finality. 

Majority of our changes are in the following files:
- all files under `espresso/` folder
- `batch_poster.go`
- `arbnode/espresso_caff_node.go`

All our tests include the prefix `TestEspresso` and `TestAuthDB`

# Build and test commands
To build the code:
```
make clean && make build
```

To run our tests:

```
gotestsum \
    --format short-verbose \
    --packages="./..." \
    --rerun-fails=1 \
    -- \
    -v \
    -timeout 45m \
    -p 1 \
    -parallel 1 \
    -run 'TestEspresso'
```

```
gotestsum \
    --format short-verbose \
    --packages="github.com/offchainlabs/nitro/espresso/authdb" \
    -- \
    -v \
    -timeout 5m \
    -p 1 \
    ./espresso/authdb... \
    -run 'TestAuthDB'
```

# Code style guidelines
- Avoid unnecessary comments when the code is self-explanatory through clear function and variable names.
- Avoid making extensive changes to the batch_poster.go file.
- All tests should start with the `TestEspresso` or `TestAuthDB` prefix.

# Testing
This is blockchain infrastructure. Bugs can cause irreversible financial losses.

- Correctness over coverage: Tests must prove the code is correct, not just hit line counts
- Requirements traceability: Each requirement should have corresponding test(s)
- Edge cases are mandatory: Boundary conditions, error paths, adversarial inputs

# Security considerations
- Dont read any environment variable file ( *.env, .env)
