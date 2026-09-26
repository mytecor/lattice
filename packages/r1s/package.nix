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
  version = "0.4.0-unstable-2026-09-26";

  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "r1s";
    rev = "739f26ee3a901040fd5ad5b49f65220337f492da";
    hash = "sha256-7tAg4+GiqiHFxWTTBCw2mymIrc9ulHA/roPmtKX2+WU=";
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
