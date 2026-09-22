# F18: Foxbridge — CDP↔Firefox protocol proxy (Juggler). Upstream
# github.com/VulpineOS/foxbridge plus the four F18 compatibility patches
# that make the original browser-use/jev-ultrafast work end-to-end over
# Camoufox (see roadmap/f18-browser-agent-stack/f18-02..f18-06).
#
# Patches (all verified against upstream commit 7dee166):
#   emulation.go  — Emulation.setFocusEmulationEnabled no-op (f18-03).
#   input.go      — Input.dispatchKeyEvent commands:[selectAll] handled
#                   natively via Runtime.evaluate (f18-04).
#   page.go       — Page.navigate waits for the Juggler frame (f18-02).
#   runtime.go    — Runtime.evaluate retries on a stale execution context
#                   during navigation (f18-02).
{ lib, buildGo127Module, fetchFromGitHub }:

buildGo127Module rec {
  pname = "foxbridge";
  version = "0.1.1-f18";

  src = fetchFromGitHub {
    owner = "VulpineOS";
    repo = "foxbridge";
    rev = "7dee166567d837ecfd0cce3664a6e03fc441e97b";
    hash = "sha256-DqG+3vGFSjErlx/RDRqOustr6f+M/Vx/0nHRc1soEcI=";
  };

  patches = [
    ./emulation.go.patch
    ./input.go.patch
    ./page.go.patch
    ./runtime.go.patch
  ];

  # ./cmd/foxbridge is the CDP server; upstream also ships ./cmd/bridge and
  # ./cmd/doctor. Build the server only — it is what the Jev stack uses.
  subPackages = [ "./cmd/foxbridge" ];

  # Computed from the upstream go.mod (google/uuid v1.6.0, gorilla/websocket
  # v1.5.3): `go mod vendor` on the pinned source, then
  # `nix hash path --type sha256 vendor` (2026-09-22).
  vendorHash = "sha256-Edr6beVlkHcHj1Jx4vxnJBeVov5sSPKO8dR1G2fQ7l8=";

  ldflags = [
    "-s"
    "-w"
  ];

  meta = {
    description = "CDP-to-Firefox Protocol proxy (Juggler) — F18 browser runtime bridge";
    homepage = "https://github.com/VulpineOS/foxbridge";
    license = lib.licenses.mit;
    mainProgram = "foxbridge";
    platforms = lib.platforms.linux;
  };
}
