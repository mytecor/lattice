{ nixpkgs, pkgs, agentrunModule }:

let
  inherit (nixpkgs) lib;

  config = (lib.nixosSystem {
    modules = [
      agentrunModule
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "node-a";
        system.stateVersion = "26.05";

        lattice.agentrun-openai = {
          enable = true;
          host = "127.0.0.1";
          port = 8787;
          defaultCwd = "/srv/projects";
          allowedRoots = [ "/srv/projects" ];
          apiKeyFile = /tmp/not-a-real-key;
          path = [ pkgs.git ];
          turnTimeout = "5m";
          sessionTtl = "2m";
          streamHeartbeat = "15s";
          backends = {
            codex = {
              command = "codex-acp";
            };
            claude = {
              command = "claude-agent-acp";
              args = [ "--debug" ];
            };
            pi = {
              command = "pi-acp";
            };
          };
          extraEnv = {
            RAD_HOME = "/persist/var/lib/radicle-peer";
            # pi-acp reads the agent's Pi config from PI_CODING_AGENT_DIR;
            # when the gateway runs as root it can reuse the root-shared Pi
            # config (same as pi-acp-daemon). Sessions stay under stateDir.
            PI_CODING_AGENT_DIR = "/root/.pi/agent";
            PI_CODING_AGENT_SESSION_DIR = "/var/lib/agentrun-openai/pi-sessions";
          };
          extraArgs = [ "--shutdown-timeout" "15s" ];
        };
      }
    ];
  }).config;

  cfg = config.lattice.agentrun-openai;
  unit = config.systemd.services.agentrun-openai;
in
assert cfg.enable;
assert cfg.port == 8787;
# Dedicated system user/group.
assert config.users.users.${cfg.user}.isSystemUser;
assert builtins.hasAttr cfg.group config.users.groups;
# Service is a foreground systemd service started at multi-user, after network.
assert lib.elem "multi-user.target" unit.wantedBy;
assert lib.elem "network-online.target" unit.after;
assert lib.elem "network-online.target" unit.wants;
# Loopback-only listener by default (no open firewall port).
assert lib.all
  (p: p != cfg.port)
  config.networking.firewall.allowedTCPPorts;

# The transparent commandLineArgs boundary exposes the generated argv.
let
  args = cfg.commandLineArgs;
in
assert lib.elem "--host" args;
assert lib.elem "127.0.0.1" args;
assert lib.elem "--port" args;
assert lib.elem "8787" args;
assert lib.elem "--session-store" args;
assert lib.elem "/var/lib/agentrun-openai/sessions.json" args;
assert lib.elem "--default-cwd" args;
assert lib.elem "/srv/projects" args;
assert lib.elem "--allowed-root" args;
assert lib.elem "--turn-timeout" args;
assert lib.elem "5m" args;
assert lib.elem "--session-ttl" args;
assert lib.elem "2m" args;
assert !(lib.elem "--api-key" args);

# ACP backends; effort variants are auto-discovered since c23d957,
# so --effort-format must not be passed (the flag is gone upstream).
assert lib.elem "--acp" args;
assert lib.elem "claude=claude-agent-acp --debug" args;
assert lib.elem "codex=codex-acp" args;
assert lib.elem "pi=pi-acp" args;
assert !(lib.elem "--effort-format" args);
assert lib.elem "--shutdown-timeout" args;
assert lib.elem "15s" args;

# Obsolete flags are not present
assert !(lib.elem "--claude-binary" args);
assert !(lib.elem "--codex-acp-binary" args);
assert !(lib.elem "--agy-binary" args);
assert !(lib.elem "--claude-thinking-budget" args);

# ExecStart is the wrapper script — systemd would pass a literal "$api_key"
# otherwise. It is a writeShellScript derivation whose path ends
# in '-agentrun-openai-wrapper'.
assert lib.hasSuffix "-agentrun-openai-wrapper" unit.serviceConfig.ExecStart;
# api key flows via LoadCredential, never as a literal in the unit's argv.
assert unit.serviceConfig.LoadCredential == [ "api-key:/tmp/not-a-real-key" ];

# PATH is built inside the wrapper script (not unit.environment, whose
# default systemd PATH would conflict); extraEnv reaches the unit verbatim.
assert unit.environment.RAD_HOME == "/persist/var/lib/radicle-peer";
assert unit.environment.PI_CODING_AGENT_DIR == "/root/.pi/agent";
assert unit.environment.PI_CODING_AGENT_SESSION_DIR == "/var/lib/agentrun-openai/pi-sessions";
# Default hardening keeps ProtectHome strict (true).
assert unit.serviceConfig.ProtectHome == true;

# --- Root-mode gateway (shared root Pi config, like pi-acp-daemon) ----------
# The module must let a node run the gateway as root with ProtectHome=read-only
# so a spawned pi-acp agent can read /root/.pi/agent and agenix secrets.
let
  rootConfig = (lib.nixosSystem {
    modules = [
      agentrunModule
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "node-root";
        system.stateVersion = "26.05";

        lattice.agentrun-openai = {
          enable = true;
          host = "127.0.0.1";
          port = 8788;
          user = "root";
          group = "root";
          protectHome = "read-only";
          path = [ pkgs.lattice.pi-acp ];
          backends.pi.command = "pi-acp";
          # pi-acp writes its session-map under $HOME/.pi/pi-acp by default;
          # with ProtectHome=read-only that path is read-only in the service
          # namespace, so point PI_ACP_DIR at the writable StateDirectory
          # (mirror modules/pi-acp-daemon).
          extraEnv = {
            PI_CODING_AGENT_DIR = "/root/.pi/agent";
            PI_ACP_DIR = "/var/lib/agentrun-openai/pi-acp";
          };
        };
      }
    ];
  }).config;

  rootUnit = rootConfig.systemd.services.agentrun-openai;
  rootArgs = rootConfig.lattice.agentrun-openai.commandLineArgs;
in
assert rootUnit.serviceConfig.User == "root";
assert rootUnit.serviceConfig.Group == "root";
assert rootUnit.serviceConfig.ProtectHome == "read-only";
# Root-mode + ProtectHome=read-only: pi-acp default-writes its session-map to
# $HOME/.pi/pi-acp (= /root/.pi/pi-acp), which is read-only in the service
# namespace, so session/new would fail with ENOENT mkdir .../session-map.json.d
# unless PI_ACP_DIR points at a writable path under the StateDirectory.
# Mirror modules/pi-acp-daemon which sets PI_ACP_DIR = stateDir/pi-acp.
assert lib.elem "read-only" [ (toString rootUnit.serviceConfig.ProtectHome) ];
assert rootConfig.lattice.agentrun-openai.extraEnv ? PI_ACP_DIR
  || throw "root-mode pi-acp backend must set extraEnv.PI_ACP_DIR to a writable StateDirectory path (e.g. /var/lib/agentrun-openai/pi-acp) when ProtectHome=read-only";
assert lib.elem "--acp" rootArgs;
assert lib.elem "pi=pi-acp" rootArgs;
assert !(lib.elem "--effort-format" rootArgs);

pkgs.runCommand "agentrun-openai-config" { } "touch $out"
