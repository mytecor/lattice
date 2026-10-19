{ nixpkgs, pkgs, agentdModule }:

let
  inherit (nixpkgs) lib;

  keyFile = pkgs.writeText "meshbus-realm-key" "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";

  config = (lib.nixosSystem {
    modules = [
      agentdModule
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "test-node";
        system.stateVersion = "26.05";

        lattice.agentd = {
          enable = true;
          realmKeyFile = keyFile;
          agentSubject = "agent.run";
          workDir = "/var/lib/agentd-work";
        };
      }
    ];
  }).config;

  cfg = config.lattice.agentd;
  unit = config.systemd.services.lattice-agentd;
  sc = unit.serviceConfig;
in
assert cfg.enable;
assert cfg.realmKeyFile == keyFile;
assert cfg.agentSubject == "agent.run";
assert cfg.workDir == "/var/lib/agentd-work";
assert cfg.maxIterations == 5;

# Service wiring and lifecycle
assert lib.elem "multi-user.target" unit.wantedBy;
assert lib.elem "network.target" unit.after;
assert lib.elem "rns-server.service" unit.after;
assert lib.elem "rns-server.service" unit.wants;

# Systemd isolation and user
assert sc.User == "agentd";
assert sc.Group == "agentd";
assert sc.StateDirectory == "lattice-agentd";
assert sc.ProtectSystem == "strict";
assert sc.ProtectHome == true;
assert sc.NoNewPrivileges == true;
assert sc.PrivateTmp == true;

pkgs.runCommand "agentd-contract-test" { } ''
  touch "$out"
''
