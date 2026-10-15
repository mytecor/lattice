{ buildPnpmCli, lib }:

buildPnpmCli {
  pname = "gemini-cli";
  version = "0.64.0";

  package = "@google/gemini-cli";
  pnpmLock = ./pnpm-lock.yaml;
  pnpmWorkspace = ./pnpm-workspace.yaml;
  pnpmDepsHash = "sha256-fhssM4HcqX7NO/oV7NAADQAfOSk6ilmfUBR8pCL/CeY=";
  executables.gemini = "bundle/gemini.js";

  description = "Google Gemini CLI — terminal AI coding agent harness with ACP support";
  homepage = "https://github.com/google-gemini/gemini-cli";
  license = lib.licenses.asl20;
  mainProgram = "gemini";
  versionCheckOutput = "0.64.0";
}
