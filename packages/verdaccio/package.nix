{ buildPnpmCli, lib }:

buildPnpmCli {
  pname = "verdaccio";
  version = "6.10.3";

  package = "verdaccio";
  pnpmLock = ./pnpm-lock.yaml;
  pnpmDepsHash = "sha256-xD963fwH6t9+SBKY7BMJQGoREdhP+28QjXvUfjHDvp0=";
  executables.verdaccio = "bin/verdaccio";
  # buildPnpmCli's default install check runs `<mainProgram> --version` and
  # compares against the version string. verdaccio prints "v6.10.3" (leading
  # "v"), so the expected output must carry the prefix, not the bare version.
  versionCheckOutput = "v6.10.3";

  description = "Lightweight private npm registry as a caching proxy";
  homepage = "https://verdaccio.org";
  license = lib.licenses.mit;
  mainProgram = "verdaccio";
}
