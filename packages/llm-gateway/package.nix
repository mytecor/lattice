{ lib, buildGo127Module, fetchFromGitHub, makeWrapper, srcOverride ? null, supportsBatch ? true }:

let
  version = "0.1.0-unstable-2026-11-26";
  revision = "77b91b7171f2cedb70f0086a69cda3763dab85bd";
  pinnedSrc = fetchFromGitHub {
    owner = "mytecor";
    repo = "llm-gateway";
    rev = revision;
    hash = "sha256-im+//ly2qMHD9chqOyR12jqgluyR8+t7PrZy5XHPIgE=";
  };
  src = if srcOverride == null then pinnedSrc else lib.cleanSource srcOverride;

  embeddingsDriver = buildGo127Module {
    pname = "llm-gateway-embeddings-driver";
    inherit version src;
    # Apply the provider until the pinned llm-gateway source includes it.
    patches = lib.optional (srcOverride == null) ./openai-embeddings-driver.patch;
    modRoot = "drivers/embeddings";
    subPackages = [ "." ];
    vendorHash = "sha256-rlug9eZpdUQx9BAhMujWJOn4H8j3oj1mluulq+x2JMU=";
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
    vendorHash = "sha256-1jFOixjj+LiMbSnBaHQYMLFzjMoSbxxRm/bl+gvc4f8=";
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
  patches = lib.optional (srcOverride == null) ./default-embedding-model.patch;
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
