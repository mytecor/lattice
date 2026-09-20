{
  stdenvNoCC,
  fetchFromGitHub,
  fetchPnpmDeps,
  nodejs,
  pnpm,
  runCommand,
  sqlite,
  writableTmpDirAsHomeHook,
  zstd,
}:

# Lattice web client for ACP (f13-01): the open-source acp-components workbench
# (package `zvzuola/acp-components`, MIT) built as a static SPA and served in
# LAN by Caddy. The upstream repo is a pnpm workspace (packages/core,
# packages/react, examples/demo); the demo (`examples/demo`) is the Vite app
# that wires everything together and is what we ship.
#
# Unlike `buildPnpmCli` (which wraps a single published npm CLI package), this
# is a full workspace build: `pnpm install --frozen-lockfile --offline` against
# the pinned `pnpm-lock.yaml` (via `fetchPnpmDeps`), then `pnpm build`
# (package-level vite build for core + react) and a final `vite build` of the
# demo. The derivation's output is the demo's `dist/` — a self-contained static
# site served by Caddy.
#
# NB: we deliberately do NOT use `pnpmConfigHook`. In the NixOS sandbox on the
# target node the hook's SQLite index reconstruction ("rebuilt from a .sql dump
# in fetcherVersion 4") did not take effect: pnpm saw the offline store as empty
# and fell back to the network (EAI_AGAIN). The manual procedure below is the
# pnpmConfigHook logic (store extraction + v11 index rebuild + arch/platform +
# store-dir + `pnpm install --offline`) reproduced explicitly, verified to reuse
# the whole FOD store with zero network access.
let
  gitSrc = fetchFromGitHub {
    owner = "zvzuola";
    repo = "acp-components";
    rev = "1708c20274c9f15ee3a072009e5ca9fd3b71a9de";
    hash = "sha256-Jn/q4fAUjL+QikWOzj5VQPBD9Ch4r9obV2uDwzYKmt8=";
  };

  # Overlay our pnpm-workspace.yaml (supply-chain relaxed, see file header)
  # onto the fetched source *before* fetchPnpmDeps, so the FOD store and the
  # later `pnpm install --offline` see the same workspace definition. This is
  # the buildPnpmCli `pnpmWorkspace` trick, done inline because this is a
  # workspace monorepo rather than a single published package.
  src = runCommand "acp-web-source" { } ''
    cp -r ${gitSrc} "$out"
    chmod -R u+w "$out"
    cp ${./pnpm-workspace.yaml} "$out/pnpm-workspace.yaml"
  '';
in
stdenvNoCC.mkDerivation (finalAttrs: {
  pname = "acp-web";
  upstreamVersion = "0.1.0";
  version = "${finalAttrs.upstreamVersion}-20260919"; # pinned upstream commit

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

  # Step 4 (f13-01): pre-configured default ACP agent (see
  # ./patch-main-ts.mjs for rationale). Pure source edit of the demo entrypoint,
  # fails loudly if upstream changes the shape it relies on.
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
