{ pkgs, lib, nix }:

# Contract: pnpm CLI packages keep their fetchPnpmDeps FOD output alive through
# a closure reference, so weekly nix-gc does not delete it and force the next
# comin rebuild to re-run `pnpm install --registry=…` against the upstream npm
# registry over the network.
#
# Why this mechanism (see packages/pnpm-cli-builder/README.md):
#  - fetchPnpmDeps is a fixed-output derivation; its output (*-pnpm-deps, the
#    compressed pnpm store) is unpacked into node_modules by pnpmConfigHook but
#    is *not* referenced by the resulting closure.
#  - With no reference and keep-outputs=false (Nix default), nix-gc deletes the
#    FOD; comin builds with --no-link (no GC roots), so the next apply re-fetches
#    npm packages from the registry. Confirmed live on mytecor-homelab 2026-09-16:
#    all five *-pnpm-deps were in the GC dead list despite a live system built
#    through them.
#  - buildPnpmCli (and packages/pi-acp) write the FOD store path into
#    $out/libexec/<pname>/pnpm-deps-store-path, so the package output references
#    the FOD and GC keeps it alive while the package is in the active closure.
#
# Implementation note: runtime check (`nix derivation show` in the build
# sandbox). Each drv path is context-stripped, so the check's own build closure
# is only `nix` — it must not pull the x86_64-linux pnpm packages into a build on
# the (Darwin) evaluation host. `nix derivation show` reads .drv files straight
# from the store without building their inputs.
#
# What this catches (a real regression, not a snapshot): the builder stops
# emitting the reference write (installPhase), or a package stops wiring
# pnpmDeps into the derivation env. Both silently reintroduce the week-long
# re-fetch cycle on the node.
let
  drvString = d: builtins.unsafeDiscardStringContext d.drvPath;
  drvArgs = {
    VERDACCIO_DRV = drvString pkgs.lattice.verdaccio;
    PI_DRV = drvString pkgs.lattice.pi;
    HYDRA_DRV = drvString pkgs.lattice.hydra-acp;
    ACP_DRV = drvString pkgs.lattice.pi-acp;
    MCP_DRV = drvString pkgs.lattice.pi-mcp-adapter;
    RETRY_DRV = drvString pkgs.lattice.pi-retry;
  };
in
pkgs.runCommand "pnpm-deps-fod-reference-check" ({
  nativeBuildInputs = [ nix ];
  passAsFile = builtins.attrNames drvArgs;
} // drvArgs) ''
  set -eu

  check_has_reference() {
    local drv="$1" label="$2"
    local show
    show=$(nix derivation show "$drv")
    echo "$show" | grep -q 'pnpm-deps-store-path' \
      || { echo "FAIL: $label drv installPhase lacks pnpm-deps-store-path write" >&2; exit 1; }
    echo "$show" | grep -qE '"pnpmDeps": "?' \
      || { echo "FAIL: $label drv env lacks pnpmDeps" >&2; exit 1; }
    echo "OK: $label keeps pnpm-deps live via closure reference"
  }

  check_has_reference "$(cat "$VERDACCIO_DRV_PATH")" "verdaccio (buildPnpmCli)"
  check_has_reference "$(cat "$PI_DRV_PATH")" "pi (buildPnpmCli)"
  check_has_reference "$(cat "$HYDRA_DRV_PATH")" "hydra-acp (buildPnpmCli)"
  # pi-acp uses fetchPnpmDeps directly (not the shared builder): same write.
  check_has_reference "$(cat "$ACP_DRV_PATH")" "pi-acp (raw fetchPnpmDeps)"

  # pi-mcp-adapter is a runCommand wrapper over a buildPnpmCli package; the
  # reference lives on the inner buildPnpmCli derivation (checked above via
  # `pi`/`verdaccio`/`hydra-acp`), which the wrapper output references through
  # its extension/node_modules symlinks (verified live 2026-09-16). The wrapper's
  # own buildCommand must string-reference that inner package output.
  echo "$(nix derivation show "$(cat "$MCP_DRV_PATH")")" \
    | grep -q 'pi-mcp-adapter-2.33.0' \
    || { echo "FAIL: pi-mcp-adapter wrapper does not reference its inner pnpm build" >&2; exit 1; }
  echo "OK: pi-mcp-adapter wrapper chains to a pnpm-deps-bearing build"

  # pi-retry (f8-03): то же — runCommand-обёртка над buildPnpmCli-пакетом
  # @geebos/pi-retry; обёртка обязана ссылаться на внутренний build, чтобы
  # pnpm-deps оставался живым в active closure.
  echo "$(nix derivation show "$(cat "$RETRY_DRV_PATH")")" \
    | grep -q 'pi-retry-0.0.2' \
    || { echo "FAIL: pi-retry wrapper does not reference its inner pnpm build" >&2; exit 1; }
  echo "OK: pi-retry wrapper chains to a pnpm-deps-bearing build"

  mkdir "$out"
''
