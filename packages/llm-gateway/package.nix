{ lib, buildGo127Module, fetchFromGitHub, makeWrapper }:

let
  version = "0.1.0-unstable-2026-11-13";
  revision = "8ce7e5824504e243394f233fc1004794dab54f7d";
  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "llm-gateway";
    rev = revision;
    hash = "sha256-ZmkZgqbbOlkvlVNJkC/gILM5zO9LMB8F9f58a50WfdI=";
  };

  # Keep the full checkout for the local replacement of the shared protocol.
  httpDriver = buildGo127Module {
    pname = "llm-gateway-http-driver";
    inherit version src;
    modRoot = "drivers/http";
    subPackages = [ "." ];
    vendorHash = "sha256-BWsBFqQYuwdi3cw0f0A9uaME7PVPDNSDqPfEG2GppDA=";
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
    go test ./driverproto ./internal/model ./internal/policy ./internal/session ./internal/structured ./internal/transform ./internal/telemetry
    runHook postCheck
  '';
  postInstall = ''
    wrapProgram "$out/bin/llm-gateway" \
      --prefix PATH : ${lib.makeBinPath [ httpDriver ]}
  '';
  passthru = {
    inherit httpDriver;
    sourceRevision = revision;
  };
  meta = {
    description = "OpenAI-compatible gateway with strict structured outputs and an HTTP driver";
    homepage = "https://github.com/mytecor/llm-gateway";
    license = lib.licenses.mit;
    mainProgram = "llm-gateway";
    platforms = lib.platforms.unix;
  };
}
