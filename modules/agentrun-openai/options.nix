{ lib, pkgs, ... }:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.agentrun-openai = {
    enable = mkEnableOption "OpenAI-compatible HTTP gateway over agentrun (persistent Claude Code / Codex / Antigravity sessions)";

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.agentrun-openai;
      defaultText = lib.literalExpression "pkgs.lattice.agentrun-openai";
      description = "Pinned agentrun-openai gateway package.";
    };

    user = mkOption {
      type = types.str;
      default = "agentrun";
      description = "Dedicated system user running the gateway.";
    };

    group = mkOption {
      type = types.str;
      default = "agentrun";
      description = "Dedicated system group owning the gateway runtime state.";
    };

    host = mkOption {
      type = types.enum [ "127.0.0.1" "::1" ];
      default = "127.0.0.1";
      description = "Loopback-only HTTP listen address.";
    };

    port = mkOption {
      type = types.port;
      default = 8787;
      description = "Loopback HTTP listen port.";
    };

    apiKeyFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Optional path to a file whose contents become the bearer API key
        (`!cat`-style runtime reference). When unset the gateway runs with no
        auth, so bind it to loopback (host/port default) or front it with a
        credentialed Caddy site.
      '';
    };

    defaultCwd = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Default working directory for agent sessions (`--default-cwd`). When
        null the gateway falls back to the service working directory. Requests
        may override it per-request via `X-Agent-CWD`.
      '';
    };

    allowedRoots = mkOption {
      type = types.listOf types.path;
      default = [ ];
      description = ''
        Allowed agent working-directory roots (`--allowed-root`, repeatable).
        When non-empty, `X-Agent-CWD` must resolve inside one of them and
        symlink escapes are rejected. Empty allows any absolute path.
      '';
    };

    sessionStoreFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Path to the persistent native-session metadata file
        (`--session-store`). The gateway stores only resume IDs, working
        directories, message counts and transcript fingerprints — never message
        text. When null the module uses a file under
        /var/lib/<stateDirectory> so native sessions survive reboots through
        the StateDirectory / impermanence.
      '';
    };

    stateDirectory = mkOption {
      type = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]*";
      default = "agentrun-openai";
      description = "systemd StateDirectory name below /var/lib.";
    };

    runtimeDirectory = mkOption {
      type = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]*";
      default = "agentrun-openai";
      description = "systemd RuntimeDirectory name below /run.";
    };

    path = mkOption {
      type = types.listOf types.package;
      default = [ ];
      description = ''
        Packages whose bin directories are put on the PATH of every spawned
        agent CLI process (injected via the service `environment.PATH`). The
        agent CLIs (`codex-acp`, `claude-agent-acp`, `pi-acp`) are resolved by
        name from the gateway's PATH, so declare them here (or rely on the
        NixOS system profile via AppendEnvironment). A minimal bash/git/tools
        profile (`pkgs.lattice.pi-tool-profile`) is recommended so sessions get
        the same shell/tool contract as the Lattice Pi runtime.
      '';
    };

    extraEnv = mkOption {
      type = types.attrsOf types.str;
      default = { };
      description = ''
        Extra environment variables merged into the gateway (and thus every
        spawned agent CLI). Use for repository/tool-specific bindings such as
        `RAD_HOME` for `git push rad://` from agent sessions.
      '';
    };

    protectHome = mkOption {
      type = types.nullOr (types.either types.bool (types.enum [ "read-only" ]));
      default = true;
      description = ''
        systemd `ProtectHome` level for the gateway service. `true` makes
        `/home`, `/root` and `/run/user` inaccessible/empty (default, strict).
        `"read-only"` mounts them read-only — required when a spawned agent
        CLI (`pi-acp`) must read the root user's shared agent config
        (`/root/.pi/agent`) and agenix secrets under `/run/agenix`. `null`
        disables the hardening. Prefer keeping the service user non-root and
        this option `true`; relax only when an agent needs the shared
        root-owned config.
      '';
    };

    turnTimeout = mkOption {
      type = types.str;
      default = "30m";
      description = "Maximum duration of one agent turn (Go duration, `--turn-timeout`).";
    };

    sessionTtl = mkOption {
      type = types.str;
      default = "10m";
      description = "Idle agent process lifetime (Go duration, `--session-ttl`).";
    };

    streamHeartbeat = mkOption {
      type = types.str;
      default = "20s";
      description = "Idle interval before a keep-alive stream delta is sent (Go duration, `--stream-heartbeat`).";
    };

    backends = mkOption {
      type = types.attrsOf (types.submodule ({ name, ... }: {
        options = {
          command = mkOption {
            type = types.str;
            default = name;
            description = "Command or binary to run for this ACP backend (e.g. `codex-acp`, `npx @agentclientprotocol/codex-acp`).";
          };

          args = mkOption {
            type = types.listOf types.str;
            default = [ ];
            description = "Additional command-line arguments passed to the backend command.";
          };
        };
      }));
      default = {
        codex = {
          command = "codex-acp";
        };
      };
      description = ''
        ACP backends registered via `--acp <id>=<command> [args...]`.
        At least one backend is required by agentrun-openai.
        Since c23d957 reasoning-effort variants are auto-discovered from
        agentrun's model catalog (the obsolete `--effort-format` flag is gone).
      '';
    };

    extraArgs = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = "Extra command-line arguments passed to the agentrun-openai binary.";
    };

    commandLineArgs = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = ''
        Effective gateway CLI argv assembled from the module options (set by
        `config.nix`, mirroring the llm-gateway `publicConfigFile` boundary so
        tests and callers can inspect the generated command line without
        parsing the ExecStart wrapper).
      '';
    };
  };
}
