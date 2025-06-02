{
  description = "Golang SDK for the Espresso Sequencer";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs";
  inputs.flake-utils.url = "github:numtide/flake-utils";
  inputs.flake-compat.url = "github:edolstra/flake-compat";
  inputs.flake-compat.flake = false;
  inputs.rust-overlay.url = "github:oxalica/rust-overlay";
  inputs.foundry.url = "github:shazow/foundry.nix/monthly";

  outputs = { flake-utils, nixpkgs, foundry, rust-overlay, ... }:
    let
      goVersion = 22; # Change this to update the whole stack
      overlays = [
        (import rust-overlay)
        (final: prev: rec {
          go = prev."go_1_${toString goVersion}";
        })
        foundry.overlay
      ];
    in
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit overlays system;
        };
        stableToolchain = pkgs.rust-bin.fromRustupToolchainFile ./rust-toolchain.toml;
          nightlyToolchain = pkgs.rust-bin.selectLatestNightlyWith (toolchain: toolchain.minimal.override {
            extensions = [ "rust-analyzer" "rustfmt" ];
        });
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
      {
        devShells =
          {
            # mkShell brings in a `cc` that points to gcc, stdenv.mkDerivation from llvm avoids this.
            default = let llvmPkgs = pkgs.llvmPackages_16; in llvmPkgs.stdenv.mkDerivation {
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

              name = "espresso-network-dev-shell";
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

                foundry-bin

                # wasm
                rust-cbindgen
                wabt

                jq
                just
                curl

                # provides abigen
                go-ethereum
              ] ++ lib.optionals stdenv.isDarwin [
                apple-sdk_11
              ] ++ lib.optionals (! stdenv.isDarwin) [
                glibc_multi.dev # provides gnu/stubs-32.h
              ];
              shellHook = shellHook + ''
                export LIBCLANG_PATH="${pkgs.llvmPackages_16.libclang.lib}/lib"
                export CC="${pkgs.clang-tools_16.clang}/bin/clang"
                export AR="${pkgs.llvm_16}/bin/llvm-ar"
              ''
                # The clang wrapper cannot find SystemConfiguration symbols on darwin
                # Undefined symbols for architecture arm64: "_SCDynamicStoreCopyProxies", referenced from:
                # system_configuration::dynamic_store::SCDynamicStore::get_proxies::h29c4032f420db6e7
                # in libespresso_crypto_helper.a(system_configuration-0133a45c6b0a8ed2.system_configuration.3319bd173d7021d9-cgu.0.rcgu.o)
                #
                # TODO: I think this shouldn't be required. We should probably
                # re-think how this flake works on darwin after the changes in
                # https://github.com/NixOS/nixpkgs/pull/346043
                + pkgs.lib.optionalString pkgs.stdenv.isDarwin
                ''
                  export NIX_LDFLAGS="-framework SystemConfiguration $NIX_LDFLAGS"
                '';
            };
          };
      });
}
