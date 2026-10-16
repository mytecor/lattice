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
  version = "0.5.1";

  # v0.5.1 adds F25-01 deterministic allocator bootstrap destinations and
  # tunnel peering fixes (default peers and detached child argument order).
  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "r1s";
    rev = "a5f22a13fea2c955df6b4bff3cd77e34634d74ab";
    hash = "sha256-Kj9gt+s2htiwRWoV8Bd9ny5VClWx7RIYksQqg8POTOI=";
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
