{
  stdenvNoCC,
  fetchFromGitHub,
  fetchPnpmDeps,
  nodejs,
  pnpm,
  pnpmConfigHook,
  runCommand,
}:

# Lattice web client for ACP (f13-01): the open-source acp-components workbench
# (package `zvzuola/acp-components`, MIT) built as a static SPA and served in
# LAN by Caddy. The upstream repo is a pnpm workspace (packages/core,
# packages/react, examples/demo); the demo (`examples/demo`) is the Vite app
# that wires everything together and is what we ship.
#
# Unlike `buildPnpmCli` (which wraps a single published npm CLI package), this
# is a full workspace build: `pnpm install --frozen-lockfile --offline` against
# the pinned `pnpm-lock.yaml` (via `fetchPnpmDeps` + `pnpmConfigHook`), then
# `pnpm build` (package-level vite build for core + react) and a final `vite
# build` of the demo. The derivation's output is the demo's `dist/` — a
# self-contained static site served by Caddy.
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
    pnpmConfigHook
  ];

  # Step 4 (f13-01): pre-configured default ACP agent (see
  # ./patch-main-ts.mjs for rationale). Pure source edit of the demo entrypoint,
  # fails loudly if upstream changes the shape it relies on.
  postPatch = ''
    node ${./patch-main-ts.mjs} examples/demo/src/main.tsx
  '';

  dontConfigure = true;

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
