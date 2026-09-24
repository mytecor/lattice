{
  stdenvNoCC,
  fetchPnpmDeps,
  nodejs,
  pnpm,
  runCommand,
  sqlite,
  writableTmpDirAsHomeHook,
  zstd,
}:
# Lattice web client for ACP (f13-01): the acp-components workbench built as a
# static SPA and served in LAN by Caddy.
#
# The source is VENDORED into ./src at the pinned upstream commit
# `zvzuola/acp-components` `1708c20274c9f15ee3a072009e5ca9fd3b71a9de` (MIT)
# — see ./README.md «Источник и внесение изменений». Two Lattice adjustments
# live in the vendored tree / build:
#   - `pnpm-workspace.yaml` is overlaid with the relaxed supply-chain copy
#     (see the file header) — this was already part of the fetchPnpmDeps
#     source, so the lock/store are unaffected.
#   - the demo entrypoint `examples/demo/src/main.tsx` keeps the pristine
#     upstream shape in `./src` (so the hash-verified pnpm FOD store is
#     byte-identical to the pre-vendor derivation), and the pre-configured
#     default ACP agent is applied at build time by ./patch-main-ts.mjs in
#     `postPatch`, exactly as before vendoring.
#
# Vendoring (rather than fetchFromGitHub) removes the network/structure
# dependency on upstream: no fetch hash to refresh, the exact source is
# auditable in this repository, and building an updated upstream is a
# deliberate in-repo edit.
#
# The repo is a pnpm workspace (packages/core, packages/react, examples/demo);
# the demo is the Vite app that wires everything together and is what we ship.
#
# NB: we deliberately do NOT use `pnpmConfigHook`. In the NixOS sandbox on the
# target node the hook's SQLite index reconstruction ("rebuilt from a .sql dump
# in fetcherVersion 4") did not take effect: pnpm saw the offline store as empty
# and fell back to the network (EAI_AGAIN). The manual procedure below is the
# pnpmConfigHook logic (store extraction + v11 index rebuild + arch/platform +
# store-dir + `pnpm install --offline`) reproduced explicitly, verified to reuse
# the whole FOD store with zero network access.
let
  src = runCommand "acp-web-src" { } ''
    cp -r ${./src} "$out"
    chmod -R u+w "$out"
  '';
in
stdenvNoCC.mkDerivation (finalAttrs: {
  pname = "acp-web";
  upstreamVersion = "0.1.0";
  version = "${finalAttrs.upstreamVersion}-20260919"; # pinned vendored commit

  inherit src;

  pnpmDeps = fetchPnpmDeps {
    inherit (finalAttrs) pname version src;
    pnpm = pnpm;
    fetcherVersion = 4;
    hash = "sha256-TwS7s8OqWKfPcbMzqCuPtfY1GZPYtEHN3WewSc3+0kA=";
  };

  nativeBuildInputs = [
    nodejs
    pnpm
    sqlite       # v11/index.db reconstruction from the .sql dump
    writableTmpDirAsHomeHook # pnpm writes config to $HOME
    zstd         # tar --zstd for the store tarball
  ];

  # f13-01 step 4 + fix: pre-configured default ACP agent (see ./patch-main-ts.mjs).
  # The vendored `./src/examples/demo/src/main.tsx` is pristine; the patch is
  # applied here at build time so the fetchPnpmDeps source above (which must
  # stay identical to the known-good store) is untouched.
  postPatch = ''
    node ${./patch-main-ts.mjs} examples/demo/src/main.tsx "${finalAttrs.version}"
  '';

  # Skip the pnpmConfigHook (see header comment); we reproduce its store setup
  # manually in configurePhase, where it is verified to work offline.
  dontPnpmConfigure = true;

  configurePhase = ''
    runHook preConfigure

    # Platform/arch must match the system the FOD store was fetched for.
    export npm_config_arch="${stdenvNoCC.targetPlatform.node.arch}"
    export pnpm_config_arch="${stdenvNoCC.targetPlatform.node.arch}"
    export npm_config_platform="${stdenvNoCC.targetPlatform.node.platform}"
    export pnpm_config_platform="${stdenvNoCC.targetPlatform.node.platform}"

    # pnpm 11: use the pinned lock without re-checking supply-chain metadata
    # (already enforced by the hash-verified FOD) and don't fail on package
    # manager resolution.
    export pnpm_config_trust_lockfile=true
    export pnpm_config_pm_on_fail=ignore

    export STORE_PATH=$(mktemp -d)
    tar --zstd -xf "$pnpmDeps/pnpm-store.tar.zst" -C "$STORE_PATH"
    chmod -R u+w "$STORE_PATH"

    # Reconstruct the SQLite index from the reproducible SQL dump (fetcherVersion 4).
    if [ -f "$STORE_PATH/v11/index.db.sql" ]; then
      sqlite3 "$STORE_PATH/v11/index.db" < "$STORE_PATH/v11/index.db.sql"
      rm "$STORE_PATH/v11/index.db.sql"
    fi

    pnpm config set reporter append-only
    pnpm config set store-dir "$STORE_PATH"
    # Prevent hard-linking across the store/build dir (sandbox may lack clone support).
    pnpm config set package-import-method clone-or-copy

    echo "Installing dependencies (offline)..."
    pnpm install --offline --ignore-scripts --frozen-lockfile

    runHook postConfigure
  '';

  buildPhase = ''
    runHook preBuild
    # Workspace build: core + react package builds (vite lib mode + tsc d.ts).
    pnpm build
    # Demo static site build (consumes the workspace packages via workspace
    # symlinks under node_modules/.pnpm).
    pnpm --filter @acp-components/demo build
    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall
    mkdir -p "$out"
    cp -r examples/demo/dist/. "$out/"
    runHook postInstall
  '';
})
