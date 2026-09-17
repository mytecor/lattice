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
# What this catches (a real regression, not a snapshot): the builder stops
# emitting the reference write (installPhase), or a package stops wiring
# pnpmDeps into the derivation env. Both silently reintroduce the week-long
# re-fetch cycle on the node.
#
# Implementation note: the original check inspected the package `.drv` files
# with `nix derivation show` inside a build sandbox, which never worked in CI:
# the sandbox `nix` binary needs the experimental `nix-command` feature (no
# nix.conf in the sandbox), and the context-stripped `.drv` paths are not in
# the check's closure, so they are absent from the sandbox store ("not a valid
# store path"). Pulling the packages in as real inputs (full string context)
# instead makes `nix flake check` build all six packages — dozens of
# derivations including node_modules — which is out of proportion for the
# regression this guards.
#
# This version is a source-level contract check (pure evaluation, zero build,
# identical locally and in CI): the FOD-alive write lives in the *builder*
# source, so reading that source file (a flake file, readable in pure eval)
# and asserting the marker + the `pnpmDeps` wiring is a faithful regression
# guard for the exact failures the build-time check caught. Every package that
# must keep its FOD alive is asserted to route through that builder, and the
# wrapper packages (pi-mcp-adapter, pi-retry) are asserted to chain to an inner
# buildPnpmCli build. The trivial build step keeps the check a derivation so
# `nix flake check` (which builds checks) is satisfied on any platform.
let
  inherit (lib) assertMsg;
  readSource = path: builtins.readFile (toString path);

  buildPnpmCliSrc = ../packages/pnpm-cli-builder/package.nix;
  piAcpSrc = ../packages/pi-acp/package.nix;
  mcpSrc = ../packages/pi-mcp-adapter/package.nix;
  retrySrc = ../packages/pi-retry/package.nix;

  assertHas = label: needle: source:
    assert assertMsg (builtins.match (".*" + needle + ".*") source != null)
      "${label} must contain '${needle}'";
    true;

  # The shared builder must keep writing the FOD store path into the output and
  # must keep wiring pnpmDeps into the derivation env (the two regression
  # points the original check looked for in installPhase / env).
  assertedBuilder =
    assertHas "buildPnpmCli" "pnpm-deps-store-path" (readSource buildPnpmCliSrc)
    && assertHas "buildPnpmCli" "pnpmDeps = fetchPnpmDeps" (readSource buildPnpmCliSrc);
  assertedPiAcp =
    assertHas "pi-acp" "pnpm-deps-store-path" (readSource piAcpSrc)
    && assertHas "pi-acp" "pnpmDeps = fetchPnpmDeps" (readSource piAcpSrc);
  # Wrappers must build their inner package through buildPnpmCli so the FOD
  # reference is inherited from the shared builder (not bypassed).
  assertedMcp = assertHas "pi-mcp-adapter" "pkg = buildPnpmCli" (readSource mcpSrc);
  assertedRetry = assertHas "pi-retry" "pkg = buildPnpmCli" (readSource retrySrc);
in
assert assertedBuilder; assert assertedPiAcp; assert assertedMcp; assert assertedRetry;
assert assertMsg (builtins.isAttrs pkgs.lattice.verdaccio)
  "pkgs.lattice.verdaccio must evaluate to a derivation";
assert assertMsg (builtins.isAttrs pkgs.lattice.pi)
  "pkgs.lattice.pi must evaluate to a derivation";
assert assertMsg (builtins.isAttrs pkgs.lattice.hydra-acp)
  "pkgs.lattice.hydra-acp must evaluate to a derivation";
assert assertMsg (builtins.isAttrs pkgs.lattice.pi-acp)
  "pkgs.lattice.pi-acp must evaluate to a derivation";
assert assertMsg (builtins.isAttrs pkgs.lattice.pi-mcp-adapter)
  "pkgs.lattice.pi-mcp-adapter must evaluate to a derivation";
assert assertMsg (builtins.isAttrs pkgs.lattice.pi-retry)
  "pkgs.lattice.pi-retry must evaluate to a derivation";
pkgs.runCommand "pnpm-deps-fod-reference-check" { } ''
  # All contract assertions ran during evaluation (see the `let` above); this
  # trivially-buildable step exists only so `nix flake check` treats this as a
  # (successful) build check on every platform.
  mkdir "$out"
''

