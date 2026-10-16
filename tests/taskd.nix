{ nixpkgs, pkgs, taskdModule }:

let
  inherit (nixpkgs) lib;

  keyFile = pkgs.writeText "meshbus-realm-key" "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";

  config = (lib.nixosSystem {
    modules = [
      taskdModule
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "test-node";
        system.stateVersion = "26.05";

        lattice.taskd = {
          enable = true;
          realmKeyFile = keyFile;
          repoPath = "/var/lib/tasks";
          ref = "HEAD";
          headSubject = "git.head";
          watchSubject = "git.watch";
          agentSubject = "agent.run";
          watchTimeout = "5s";
        };
      }
    ];
  }).config;

  cfg = config.lattice.taskd;
  unit = config.systemd.services.lattice-taskd;
  sc = unit.serviceConfig;
in
assert cfg.enable;
assert cfg.realmKeyFile == keyFile;
assert cfg.repoPath == "/var/lib/tasks";
assert cfg.ref == "HEAD";
assert cfg.headSubject == "git.head";
assert cfg.watchSubject == "git.watch";
assert cfg.agentSubject == "agent.run";
assert cfg.watchTimeout == "5s";

# Service wiring and lifecycle
assert lib.elem "multi-user.target" unit.wantedBy;
assert lib.elem "network.target" unit.after;
assert lib.elem "rns-server.service" unit.after;
assert lib.elem "lattice-git-watchd.service" unit.after;
assert lib.elem "rns-server.service" unit.wants;

# Systemd isolation and user
assert sc.User == "taskd";
assert sc.Group == "taskd";
assert sc.StateDirectory == "lattice-taskd";
assert sc.ProtectSystem == "strict";
assert sc.ProtectHome == true;
assert sc.NoNewPrivileges == true;
assert sc.PrivateTmp == true;

pkgs.runCommand "taskd-contract-test" { } ''
  touch "$out"
''
