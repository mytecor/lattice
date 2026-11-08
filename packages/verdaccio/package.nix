{ buildPnpmCli, lib }:

buildPnpmCli {
  pname = "verdaccio";
  version = "7.0.2";

  package = "verdaccio";
  pnpmLock = ./pnpm-lock.yaml;
  pnpmWorkspace = ./pnpm-workspace.yaml;
  pnpmDepsHash = "sha256-Tf62U7rlWSw2Bo/MbCl932Dv96j3ruCo78ixC1oNdG0=";
  executables.verdaccio = "bin/verdaccio";
  # buildPnpmCli's default install check runs `<mainProgram> --version` and
  # compares against the version string. verdaccio prints "v7.0.2" (leading
  # "v"), so the expected output must carry the prefix, not the bare version.
  versionCheckOutput = "v7.0.2";

  description = "Lightweight private npm registry as a caching proxy";
  homepage = "https://verdaccio.org";
  license = lib.licenses.mit;
  mainProgram = "verdaccio";
}
