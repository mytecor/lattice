{
  lib,
  go,
  buildGoModule,
  fetchFromGitHub,
}:
# Lattice execution backend (F10): `r1s` client + `r1sd` allocator.
# Needs go >= 1.27.1; the flake passes
# go_1_27 = 1.27.1 from the main nixpkgs pin.
(buildGoModule.override { inherit go; }) rec {
  pname = "r1s";
  version = "0.5.0";

  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "r1s";
    rev = "832a2744b06d4ebdbb5d601e9a26cc7bff4048d7";
    hash = "sha256-VSi52qpRkuJc0wY2hv5H0J0DzmmBM244uapnem5ORdg=";
  };

  vendorHash = "sha256-WArtt0x1GJXXWiFAMyBSDCuOSsgIkuVRcgmuq/KnbCE=";

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
