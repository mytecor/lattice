{
  lib,
  go,
  buildGoModule,
  fetchFromGitHub,
}:
# Lattice execution backend (F10): `r1s` client + `r1sd` allocator.
# Needs go >= 1.27.1 (go.mod + Reticulum-Go v1.2.0); the flake passes
# go_1_27 = 1.27.1 from the main nixpkgs pin.
(buildGoModule.override { inherit go; }) rec {
  pname = "r1s";
  version = "0.1.0-unstable-2026-09-23";

  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "r1s";
    rev = "92ee1022e2b61d885da4ed27cce3eac483ac94a6";
    hash = "sha256-SWtU+gz3StypcpnBlBsdl5WVe10MsU2En/rZp9BCXXI=";
  };

  vendorHash = "sha256-lwsRn5JlCguU9mIgtQF+4O+xeKmiYzX+TIW48X9HuUg=";

  # Both binaries are produced by `go build ./cmd/r1s ./cmd/r1sd`.
  subPackages = [ "cmd/r1s" "cmd/r1sd" ];

  doCheck = true;

  ldflags = [
    "-s"
    "-w"
  ];

  meta = {
    description = "Decentralized OCI workload execution fabric over RNS: r1s client + r1sd allocator";
    homepage = "https://github.com/mytecor/r1s";
    license = lib.licenses.mit;
    mainProgram = "r1s";
    platforms = lib.platforms.linux;
  };
}
