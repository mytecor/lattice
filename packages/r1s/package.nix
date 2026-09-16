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
  version = "0.1.0-unstable-2026-09-16";

  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "r1s";
    rev = "b40a279f983336ee948edab98f18b1d0b18c5e40";
    hash = "sha256-7FEbSx8QyQlyW56dLipsNVXYuecZk40F2ecQRYe4m7k=";
  };

  vendorHash = "sha256-KNoJZa8BEN/j0sTaJgFnmEVdI361I8Za2qCy6U/wc+M=";

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
