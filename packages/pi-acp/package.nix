{
  fetchFromGitHub,
  fetchPnpmDeps,
  lib,
  makeWrapper,
  nodejs,
  pi,
  pnpm,
  pnpmConfigHook,
  runCommand,
  stdenvNoCC,
}:

let
  pname = "pi-acp";
  version = "0.1.0-unstable-2026-09-09";

  upstreamSource = fetchFromGitHub {
    owner = "regadas";
    repo = "pi-acp";
    rev = "34865aeff52cc529ee6b3433a1aac6887b2c80e2";
    hash = "sha256-0PZ/yw7Y1iffQibI4MYL6vMFdoupkcdqlAzt9xiNjYE=";
  };

  source = runCommand "${pname}-${version}-pnpm-source" { } ''
    mkdir -p "$out"
    cp -R ${upstreamSource}/. "$out/"
    cp ${./pnpm-lock.yaml} "$out/pnpm-lock.yaml"
    # Lattice patch: upstream svkozak/pi-acp accepts mcpServers (stores them, does
    # not wire them to pi; MCP is provided inside pi by pi-mcp-adapter), but the
    # maintained regadas fork rejects them with MCP_SERVERS_UNSUPPORTED, failing
    # session/new for clients that cannot omit the field. Accept and warn instead.
    chmod -R u+w "$out"
    (cd "$out" && patch -p1 < ${./mcp-servers-accepted.patch}) \
      || { echo "pi-acp: mcp-servers-accepted.patch did not apply" >&2; exit 1; }
  '';
in
stdenvNoCC.mkDerivation (finalAttrs: {
  inherit pname version source;
  src = source;

  pnpmDeps = fetchPnpmDeps {
    inherit (finalAttrs) pname src version;
    hash = "sha256-Rky1naHIESXtQtMP1zD+235X3p8vi/BXc+Q8tgRc100=";
    fetcherVersion = 4;
    pnpm = pnpm;
  };

  nativeBuildInputs = [
    makeWrapper
    nodejs
    pnpm
    pnpmConfigHook
  ];

  strictDeps = true;

  buildPhase = ''
    runHook preBuild
    pnpm run build
    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall

    pnpm prune --prod
    mkdir -p "$out/bin" "$out/libexec/${pname}"
    cp -R dist node_modules package.json pnpm-lock.yaml "$out/libexec/${pname}/"
    makeWrapper ${lib.getExe nodejs} "$out/bin/pi-acp" \
      --add-flags "$out/libexec/${pname}/dist/index.js" \
      --prefix PATH : ${lib.makeBinPath [ pi ]}

    runHook postInstall
  '';

  doInstallCheck = true;
  installCheckPhase = ''
    runHook preInstallCheck
    "$out/bin/pi-acp" --help >/dev/null
    runHook postInstallCheck
  '';

  passthru = {
    inherit source upstreamSource;
    pnpmLock = ./pnpm-lock.yaml;
  };

  meta = {
    description = "ACP adapter for the Pi coding agent";
    homepage = "https://github.com/regadas/pi-acp";
    license = lib.licenses.mit;
    mainProgram = "pi-acp";
    platforms = nodejs.meta.platforms;
  };
})
