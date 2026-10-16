{ nixpkgs, pkgs, gitWatchdModule }:

let
  inherit (nixpkgs) lib;

  keyFile = pkgs.writeText "meshbus-realm-key" "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";

  config = (lib.nixosSystem {
    modules = [
      gitWatchdModule
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "test-node";
        system.stateVersion = "26.05";

        lattice.git-watchd = {
          enable = true;
          realmKeyFile = keyFile;
          allowedDir = "/var/lib";
          pollInterval = "500ms";
        };
      }
    ];
  }).config;

  cfg = config.lattice.git-watchd;
  unit = config.systemd.services.lattice-git-watchd;
  sc = unit.serviceConfig;
in
assert cfg.enable;
assert cfg.realmKeyFile == keyFile;
assert cfg.allowedDir == "/var/lib";
assert cfg.pollInterval == "500ms";

# Service wiring and lifecycle
assert lib.elem "multi-user.target" unit.wantedBy;
assert lib.elem "network.target" unit.after;
assert lib.elem "rns-server.service" unit.after;
assert lib.elem "rns-server.service" unit.wants;

# Systemd isolation and user
assert sc.User == "git-watchd";
assert sc.Group == "git-watchd";
assert sc.StateDirectory == "lattice-git-watchd";
assert sc.ProtectSystem == "strict";
assert sc.ProtectHome == true;
assert sc.NoNewPrivileges == true;
assert sc.PrivateTmp == true;

pkgs.runCommand "git-watchd-contract-test" { } ''
  touch "$out"
''
