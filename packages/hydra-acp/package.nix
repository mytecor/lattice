{ buildPnpmCli, lib }:

buildPnpmCli {
  pname = "hydra-acp";
  version = "0.1.183";

  package = "@hydra-acp/cli";
  pnpmLock = ./pnpm-lock.yaml;
  pnpmDepsHash = "sha256-Yzk5efSrfoqYdMzLUSrb6ZJNBs0/IUMdicz7xXIhRXA=";
  executables = {
    hydra = "dist/cli.js";
    hydra-acp = "dist/cli.js";
    hydra-acp-daemon = "dist/daemon.js";
  };

  description = "Multi-client ACP session daemon";
  homepage = "https://github.com/smagnuso/hydra-acp";
  license = lib.licenses.mit;
  mainProgram = "hydra-acp";
  versionCheckOutput = "hydra-acp 0.1.183";
}
