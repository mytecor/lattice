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

  # v0.5.1 adds F25-01 deterministic allocator bootstrap destinations: a fresh
  # run client reaches known allocator destinations immediately instead of
  # waiting for the periodic 5m announce, fixing the non-deterministic discovery
  # that blocked the f10-04 end-to-end smoke (`no usable offer` before the next
  # announcement). go.mod/go.sum are unchanged from v0.5.0, so vendorHash stays.
  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "r1s";
    rev = "3ea26dc7f039293f596d5aac19a13858d7193c65";
    hash = "sha256-oAHiBmuZT+XP//ImUTm3wx30qX8WC81u5MfRm+GQfvo=";
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
