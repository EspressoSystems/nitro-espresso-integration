{
  description = "A Nix-flake-based Nitro development environment";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  inputs.flake-utils.url = "github:numtide/flake-utils";
  inputs.flake-compat.url = "github:edolstra/flake-compat";
  inputs.flake-compat.flake = false;
  inputs.rust-overlay.url = "github:oxalica/rust-overlay";
  inputs.foundry.url = "github:shazow/foundry.nix/stable";
  inputs.pre-commit-hooks.url = "github:cachix/pre-commit-hooks.nix";

  outputs = { self, flake-utils, nixpkgs, foundry, rust-overlay, pre-commit-hooks, ... }:
    let
      goVersion = 25; # Change this to update the whole stack
      overlays = [
        (import rust-overlay)
        (final: prev: rec {
          go = prev."go_1_${toString goVersion}";
          nodejs = prev.nodejs_24;
          yarn = (prev.yarn.override { inherit nodejs; });
          pnpm = (prev.pnpm.override { inherit nodejs; });
        })
        foundry.overlay
      ];
    in
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit overlays system;
        };
        stableToolchain = pkgs.rust-bin.stable."1.88.0".minimal.override {
          extensions = [ "rustfmt" "clippy" "llvm-tools-preview" "rust-src" ];
          targets = [ "wasm32-unknown-unknown" "wasm32-wasip1" ];
        };
        nightlyToolchain = pkgs.rust-bin.nightly."2024-12-17".minimal.override {
          extensions = [ "rust-src" ];
          targets = [ "wasm32-unknown-unknown" "wasm32-wasip1" ];
        };
        # A script that calls nightly cargo if invoked with `+nightly`
        # as the first argument, otherwise it calls stable cargo.
        cargo-with-nightly = pkgs.writeShellScriptBin "cargo" ''
          if [[ "$1" == "+nightly" ]]; then
            shift
            # Prepend nightly toolchain directory containing cargo, rustc, etc.
            exec env PATH="${nightlyToolchain}/bin:$PATH" cargo "$@"
          fi
          exec ${stableToolchain}/bin/cargo "$@"
        '';
        shellHook = ''
          # Prevent cargo aliases from using programs in `~/.cargo` to avoid conflicts
          # with rustup installations.
          export CARGO_HOME=$HOME/.cargo-nix
          export DOCKER_BUILDKIT=1

          # Create a target directory and ensure lib64 is a symlink to lib.
          # Individual build steps may target either directory and later
          # create the symlink making some build outputs inaccessible.
          mkdir -p target/lib
          ln -sf lib target/lib64
        ''
        + pkgs.lib.optionalString pkgs.stdenv.isDarwin ''
          # Fix docker-buildx command on OSX. Can we do this in a cleaner way?
          mkdir -p ~/.docker/cli-plugins
          # Check if the file exists, otherwise symlink
          test -f $HOME/.docker/cli-plugins/docker-buildx || ln -sn $(which docker-buildx) $HOME/.docker/cli-plugins
        '';
      in
      with pkgs; {
        checks = {
          pre-commit-check = pre-commit-hooks.lib.${system}.run {
            src = ./.;
            hooks = {
              golangci-lint = {
                enable = true;
                entry = "golangci-lint run --new-from-rev=HEAD --fix";
                pass_filenames = false;
                types = [ "go" ];
              };
            };
          };
        };
        devShells =
          {
            # mkShell brings in a `cc` that points to gcc, stdenv.mkDerivation from llvm avoids this.
            default = let llvmPkgs = pkgs.llvmPackages; in llvmPkgs.stdenv.mkDerivation {
              hardeningDisable = [
                # By default stack protection is enabled by the clang wrapper but I
                # think it's not supported for wasm compilation. It causes this
                # error:
                #
                #   Undefined stack protector symbols: __stack_chk_guard ...
                #   in arbitrator/wasm-libraries/soft-float/SoftFloat/build/Wasm-Clang/extF80_div.o
                "stackprotector"
                # See https://github.com/NixOS/nixpkgs/pull/256956#issuecomment-2351143479
                "zerocallusedregs"
              ];

              name = "espresso-nitro-dev-shell";
              buildInputs = with pkgs; [
                cmake
                cargo-with-nightly
                stableToolchain
                openssl
                pkg-config

                llvmPkgs.clang
                llvmPkgs.bintools # provides wasm-ld

                go
                # goimports, godoc, etc.
                gotools
                golangci-lint
                gotestsum

                # Node
                nodejs
                yarn

                python3
                wget

                # wasm
                rust-cbindgen
                wabt

                # for dynamic linking
                curl

                # Docker
                docker-compose # provides the `docker-compose` command
                docker-buildx
                docker-credential-helpers # for `docker-credential-osxkeychain` command

                foundry-bin

                # provides abigen
                go-ethereum

                # Needed to avoid some error on Linux related to glibc
                git

                # just
                just

                pre-commit
              ] ++ lib.optionals stdenv.isDarwin [
              ] ++ lib.optionals (! stdenv.isDarwin) [
                glibc_multi.dev # provides gnu/stubs-32.h
              ];
              shellHook = shellHook + ''
                export LIBCLANG_PATH="${llvmPkgs.libclang.lib}/lib"
              ''
                # Not sure why these lines are needed to avoid mixed SDK.
                + pkgs.lib.optionalString pkgs.stdenv.isDarwin
                ''
                export SDKROOT="$SDKROOT_FOR_TARGET"
                export DEVELOPER_DIR="$DEVELOPER_DIR_FOR_TARGET"
                export MACOSX_DEPLOYMENT_TARGET=12.3
                ''
                + self.checks.${system}.pre-commit-check.shellHook;
            };
          };
      });
}
