{ buildPnpmCli, lib }:

buildPnpmCli {
  pname = "pi-coding-agent";
  version = "1.5.0";

  package = "@earendil-works/pi-coding-agent";
  pnpmLock = ./pnpm-lock.yaml;
  pnpmWorkspace = ./pnpm-workspace.yaml;
  pnpmDepsHash = "sha256-uvXd0UUTaGxTnwnGPAAMpprflQJSxDieqsRh8QIOwXM=";
  executables.pi = "dist/bundle/cli.js";

  description = "Minimal terminal coding harness";
  homepage = "https://github.com/earendil-works/pi";
  license = lib.licenses.mit;
  mainProgram = "pi";
}
