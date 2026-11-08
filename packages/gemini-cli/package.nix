{ buildPnpmCli, lib }:

buildPnpmCli {
  pname = "gemini-cli";
  version = "0.67.0";

  package = "@google/gemini-cli";
  pnpmLock = ./pnpm-lock.yaml;
  pnpmWorkspace = ./pnpm-workspace.yaml;
  pnpmDepsHash = "sha256-X7Gjt9grdO5M/7jWI++w7xEFv+sz2DQylurrnA4/7cg=";
  executables.gemini = "bundle/gemini.js";

  description = "Google Gemini CLI — terminal AI coding agent harness with ACP support";
  homepage = "https://github.com/google-gemini/gemini-cli";
  license = lib.licenses.asl20;
  mainProgram = "gemini";
  versionCheckOutput = "0.67.0";
}
