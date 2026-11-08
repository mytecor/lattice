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

  # v0.5.1 + F24-13 meshbus Node adoption + Reticulum-Go announce debug spam fix.
  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "r1s";
    rev = "bb9ecc2fc0be1cc70d23f7001d6d0e0b7be6defd";
    hash = "sha256-lPaezeNexN96FigzSZyeKylynaBU+7lFh4yXdojiOZY=";
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
