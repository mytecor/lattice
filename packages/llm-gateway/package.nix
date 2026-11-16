{ lib, buildGo127Module, fetchFromGitHub, makeWrapper }:

let
  version = "0.1.0-unstable-2026-11-16";
  revision = "939604d888b4bfa7444874349c2146b2dd6481ec";
  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "llm-gateway";
    rev = revision;
    hash = "sha256-xm1g20Ze+ANSO/VD827eghv4JGjR11+s+eME2QWjV+A=";
  };

  jevDriver = buildGo127Module {
    pname = "llm-gateway-jev-driver";
    inherit version src;
    modRoot = "drivers/jev";
    subPackages = [ "." ];
    vendorHash = "sha256-XKtqBWM6ToUOhErAPjdgcq6psx0O1xEHp4K97NU1dTY=";
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
    vendorHash = "sha256-mGFswlI92k9cszZYsNHa+MpavQHQ/lw50fXJxEl1J6k=";
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
      --prefix PATH : ${lib.makeBinPath [ httpDriver jevDriver ]}
  '';
  passthru = {
    inherit httpDriver jevDriver;
    sourceRevision = revision;
  };
  meta = {
    description = "OpenAI-compatible gateway with strict structured outputs and HTTP/JEV drivers";
    homepage = "https://github.com/mytecor/llm-gateway";
    license = lib.licenses.mit;
    mainProgram = "llm-gateway";
    platforms = lib.platforms.unix;
  };
}
