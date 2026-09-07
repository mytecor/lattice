{ buildPnpmCli, lib }:

buildPnpmCli {
  pname = "pi-coding-agent";
  version = "0.85.1";

  package = "@earendil-works/pi-coding-agent";
  pnpmLock = ./pnpm-lock.yaml;
  pnpmDepsHash = "sha256-MbYRMvkyCoH5vCuX3Xm7vTB6073qmUqOL/PAOkG7qRc=";
  executables.pi = "dist/bundle/cli.js";

  description = "Minimal terminal coding harness";
  homepage = "https://github.com/earendil-works/pi";
  license = lib.licenses.mit;
  mainProgram = "pi";
}
