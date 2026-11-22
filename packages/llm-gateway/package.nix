{ lib, buildGo127Module, fetchFromGitHub, makeWrapper, srcOverride ? null, supportsBatch ? true }:

let
  version = "0.1.0-unstable-2026-11-23";
  revision = "21badf6dcd511b5bb7f4c1ebe15b58fdb1684c38";
  pinnedSrc = fetchFromGitHub {
    owner = "mytecor";
    repo = "llm-gateway";
    rev = revision;
    hash = "sha256-VAGc9+NKRh2LJsKrwWx/aW22jCz9nu62kaaVqBOr9zs=";
  };
  src = if srcOverride == null then pinnedSrc else lib.cleanSource srcOverride;

  embeddingsDriver = buildGo127Module {
    pname = "llm-gateway-embeddings-driver";
    inherit version src;
    modRoot = "drivers/embeddings";
    subPackages = [ "." ];
    vendorHash = "sha256-wvp99zeGoVA0aGWnsA2gB7WVbC2PJcsbsTjrhmYsT38=";
    postInstall = ''
      mv "$out/bin/embeddings" "$out/bin/embeddings-driver"
    '';
    doCheck = true;
    meta.mainProgram = "embeddings-driver";
  };

  jevDriver = buildGo127Module {
    pname = "llm-gateway-jev-driver";
    inherit version src;
    modRoot = "drivers/jev";
    subPackages = [ "." ];
    vendorHash = "sha256-WkWGmtwzW6XbF4KsFpJqE+Rujk+ob2Gml2X/MM+ScVU=";
    postInstall = ''
      mv "$out/bin/jev" "$out/bin/jev-driver"
    '';
    doCheck = true;
    meta.mainProgram = "jev-driver";
  };

  # Keep the full checkout for the local replacement of the shared protocol.
  httpDriver = buildGo127Module {
    pname = "llm-gateway-http-driver";
    inherit version src;
    modRoot = "drivers/http";
    subPackages = [ "." ];
    vendorHash = "sha256-uLP+K8KoDAKPFGOAu9auNRkUs8qljDlYDnREegnDK6Y=";
    postInstall = ''
      mv "$out/bin/http" "$out/bin/http-driver"
    '';
    doCheck = true;
    meta.mainProgram = "http-driver";
  };
in
buildGo127Module {
  pname = "lattice-llm-gateway";
  inherit version src;
  vendorHash = "sha256-qVoj03LNLbdoCUAOydK7oEHsuZ1BZ6Z2jwYB3gPOfrw=";
  subPackages = [ "cmd/llm-gateway" ];
  nativeBuildInputs = [ makeWrapper ];
  doCheck = true;
  # Core unit packages are independent of the installed drivers. The packaged
  # HTTP boundary is exercised separately by the Lattice runtime smoke test.
  checkPhase = ''
    runHook preCheck
    go test ./driverproto/... ./internal/model ./internal/policy ./internal/session ./internal/structured ./internal/transform ./internal/telemetry
    runHook postCheck
  '';
  postInstall = ''
    wrapProgram "$out/bin/llm-gateway" \
      --prefix PATH : ${lib.makeBinPath [ httpDriver jevDriver embeddingsDriver ]}
  '';
  passthru = {
    inherit httpDriver jevDriver embeddingsDriver supportsBatch;
    sourceRevision = revision;
  };
  meta = {
    description = "OpenAI-compatible gateway with strict structured outputs and HTTP/JEV/embeddings drivers";
    homepage = "https://github.com/mytecor/llm-gateway";
    license = lib.licenses.mit;
    mainProgram = "llm-gateway";
    platforms = lib.platforms.unix;
  };
}
