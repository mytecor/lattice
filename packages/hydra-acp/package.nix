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
  # client support. Rationale: roadmap/f8-pi-runtime/f8-06; re-verified by
  # tests/acp-ingress-smoke.mjs (never-prompted session must appear in session/list).
  postInstall = ''
    target="$out/libexec/hydra-acp/node_modules/@hydra-acp/cli/dist/daemon.js"
    sed -i 's/e\.manager\.list({cwd:f\.cwd})/e.manager.list({cwd:f.cwd,includeNonInteractive:!0})/' "$target"
    grep -q 'e.manager.list({cwd:f.cwd,includeNonInteractive:!0})' "$target" \
      || { echo "hydra-acp: session/list includeNonInteractive patch did not apply" >&2; exit 1; }

    # Lattice patch (f15-01): ALWAYS force the daemon defaultCwd in session/new,
    # ignoring whatever cwd the client sent. The WS schema makes cwd mandatory
    # (a client may send "", "/", or any absolute path) and upstream uses it
    # verbatim, so a stateless client (e.g. acp-ui, Ferngeist) decides the
    # session's working directory — which breaks the node dev-loop: an empty or
    # root path puts the agent outside the Lattice checkout. Every Lattice ACP
    # session must operate in the node's working copy, so we unconditionally
    # replace the client-supplied cwd with fe(this.defaultCwd). `fe` is the
    # daemon's expandHome (already used by resolveResurrectTarget) and
    # this.defaultCwd is set in the manager constructor (default "~" -> home,
    # or the configured /var/lib/lattice-workspace/lattice). Rationale:
    # roadmap/f15-node-dev-loop/f15-01; behaviour covered by the pi-acp-daemon
    # module docs. Client-side acp-ui patching is therefore unnecessary.
    sed -i 's/async create(e){let t=await this.registry.getAgent(e.agentId);/async create(e){e={...e,cwd:fe(this.defaultCwd)};let t=await this.registry.getAgent(e.agentId);/' "$target"
    grep -q 'async create(e){e={...e,cwd:fe(this.defaultCwd)};let t=await this.registry.getAgent(e.agentId);' "$target" \
      || { echo "hydra-acp: session/new always-force defaultCwd patch did not apply" >&2; exit 1; }
  '';
}
