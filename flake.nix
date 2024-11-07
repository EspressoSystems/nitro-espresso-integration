{
  description = "A Nix-flake-based Go 1.21 development environment";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
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
          # Overlaying nodejs here to ensure nodePackages use the desired
          # version of nodejs. Offchainlabs suggests nodejs v18 in the docs.
          nodejs = prev.nodejs_18;
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
          config = {
            permittedInsecurePackages = [ "nodejs-16.20.2" ];
          };
        };
        stableToolchain = pkgs.rust-bin.stable.latest.minimal.override {
          extensions = [ "rustfmt" "clippy" "llvm-tools-preview" "rust-src" ];
          targets = [ "wasm32-unknown-unknown" "wasm32-wasi" ];
        };
        nightlyToolchain = pkgs.rust-bin.selectLatestNightlyWith (
          toolchain: toolchain.default.override { extensions = [ "rust-src" ]; }
        );
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
            # This shell is only used for one make recipe because the other
            # shell is not able to build one recipe and we haven't managed to
            # come up with a dev shell that works for everything on OSX.
            #
            # See ./scripts/build-wasm-on-macos-with-nix for how to use it.
            #
            # With nix the `clang` command is a wrapper that does not understand
            # some of the arguments that are passed to it during the build. This
            # dev shell uses the unwrapped clang command and sets the include
            # directory manually via `CPATH`.
            wasm = pkgs.mkShell {
              # By default clang-unwrapped does not find its resource dir. See
              # https://discourse.nixos.org/t/why-is-the-clang-resource-dir-split-in-a-separate-package/34114
              CPATH = "${pkgs.llvmPackages_16.libclang.lib}/lib/clang/16/include";
              packages = with pkgs; [
                stableToolchain

                llvmPackages_16.clang-unwrapped # provides clang without wrapper
                llvmPackages_16.bintools # provides wasm-ld
                cmake
                wabt # wasm2wat, wat2wasm, etc.

                # Docker
                docker-compose # provides the `docker-compose` command
                docker-buildx
                docker-credential-helpers # for `docker-credential-osxkeychain` command
              ];

              # Ensure the unwrapped clang is used by default.
              shellHook = shellHook + ''
                export PATH="${pkgs.llvmPackages_16.clang-unwrapped}/bin:$PATH"
              '';
            };

            # mkShell brings in a `cc` that points to gcc, stdenv.mkDerivation from llvm avoids this.
            default = let llvmPkgs = pkgs.llvmPackages_16; in llvmPkgs.stdenv.mkDerivation {
              # By default stack protection is enabled by the clang wrapper but I
              # think it's not supported for wasm compilation. It causes this
              # error:
              #
              #   Undefined stack protector symbols: __stack_chk_guard ...
              #   in arbitrator/wasm-libraries/soft-float/SoftFloat/build/Wasm-Clang/extF80_div.o
              hardeningDisable = [ "stackprotector" ];

              name = "espresso-nitro-dev-shell";
              buildInputs = with pkgs; [
                cmake
                cargo-with-nightly
                stableToolchain

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

                # Docker
                docker-compose # provides the `docker-compose` command
                docker-buildx
                docker-credential-helpers # for `docker-credential-osxkeychain` command

                foundry-bin

                # provides abigen
                go-ethereum
              ] ++ lib.optionals stdenv.isDarwin [
                darwin.libobjc
                darwin.IOKit
                darwin.apple_sdk.frameworks.CoreFoundation
              ] ++ lib.optionals (! stdenv.isDarwin) [
                glibc_multi.dev # provides gnu/stubs-32.h
              ];
              shellHook = shellHook + ''
                export LIBCLANG_PATH="${pkgs.llvmPackages_16.libclang.lib}/lib"
                export CC="${pkgs.clang-tools_16.clang}/bin/clang"
                export AR="${pkgs.llvm_16}/bin/llvm-ar"
              '';
            };
          };
      });
}
