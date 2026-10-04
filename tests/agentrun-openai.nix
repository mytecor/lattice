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
          extraEnv.RAD_HOME = "/persist/var/lib/radicle-peer";
          turnTimeout = "5m";
          sessionTtl = "2m";
          streamHeartbeat = "15s";
          claudeThinkingBudget = 0;
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

# ExecStart is the wrapper script — systemd would pass a literal "$api_key"
# otherwise. It is a writeShellScript derivation whose path ends
# in '-agentrun-openai-wrapper'.
assert lib.hasSuffix "-agentrun-openai-wrapper" unit.serviceConfig.ExecStart;
# api key flows via LoadCredential, never as a literal in the unit's argv.
assert unit.serviceConfig.LoadCredential == [ "api-key:/tmp/not-a-real-key" ];

# PATH is built inside the wrapper script (not unit.environment, whose
# default systemd PATH would conflict); extraEnv reaches the unit verbatim.
assert unit.environment.RAD_HOME == "/persist/var/lib/radicle-peer";

pkgs.runCommand "agentrun-openai-config" { } "touch $out"
