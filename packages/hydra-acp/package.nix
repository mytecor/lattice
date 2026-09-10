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

  # Lattice patch (f8-06): ACP `session/list` must return every session, including
  # never-prompted (non-interactive) ones. Upstream 0.1.183 hides them (manager.list
  # is called without includeNonInteractive), so a freshly created session is
  # invisible until its first turn; a reconnecting client then creates a new session
  # instead of resuming, and warm sessions/agents accumulate silently. Forcing the
  # flag on the daemon-side manager call fixes listing on the server, independent of
  # client support. Rationale: docs/roadmap/f8-pi-runtime/f8-06; re-verified by
  # tests/acp-ingress-smoke.mjs (never-prompted session must appear in session/list).
  postInstall = ''
    target="$out/libexec/hydra-acp/node_modules/@hydra-acp/cli/dist/daemon.js"
    sed -i 's/e\.manager\.list({cwd:f\.cwd})/e.manager.list({cwd:f.cwd,includeNonInteractive:!0})/' "$target"
    grep -q 'e.manager.list({cwd:f.cwd,includeNonInteractive:!0})' "$target" \
      || { echo "hydra-acp: session/list includeNonInteractive patch did not apply" >&2; exit 1; }
  '';
}
