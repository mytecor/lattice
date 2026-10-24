{ nixpkgs, pkgs, agentdModule }:
let
  inherit (nixpkgs) lib;
  makeConfig = settings: (lib.nixosSystem {
    modules = [
      agentdModule
      { nixpkgs.hostPlatform = "x86_64-linux"; system.stateVersion = "26.05";
        nixpkgs.overlays = [ (_: _: { lattice.agentd = pkgs.writeShellScriptBin "agentd" "exit 0"; }) ];
        lattice.agentd = { enable = true; realmKeyFile = "/run/keys/realm"; } // settings;
      }
    ];
  }).config;
  remote = makeConfig {
    runtime = "r1s";
    r1s.clusterKeyFile = "/run/keys/cluster";
    r1s.agentEnvironmentFile = "/run/keys/agent-environment";
    r1s.bootstrapDestinations = [ "11111111111111111111111111111111" ];
  };
  invalid = makeConfig { runtime = "r1s"; };
  unit = remote.systemd.services.lattice-agentd;
  defaultUnit = (makeConfig { }).systemd.services.lattice-agentd;
in
assert builtins.elem "multi-user.target" unit.wantedBy;
assert builtins.elem "rns-server.service" unit.after;
assert unit.serviceConfig.NoNewPrivileges;
assert unit.serviceConfig.ProtectSystem == "strict";
assert unit.serviceConfig.ProtectHome;
assert unit.serviceConfig.PrivateTmp;
assert lib.any (a: !a.assertion && lib.hasInfix "clusterKeyFile" a.message) invalid.assertions;
assert builtins.elem "r1s-cluster-key:/run/keys/cluster" unit.serviceConfig.LoadCredential;
assert builtins.elem "agent-environment:/run/keys/agent-environment" unit.serviceConfig.LoadCredential;
assert !(lib.any (c: lib.hasPrefix "r1s-cluster-key:" c) defaultUnit.serviceConfig.LoadCredential);
pkgs.runCommand "agentd-runtime-wiring" { } ''
  script=${unit.serviceConfig.ExecStart}
  ${pkgs.bash}/bin/bash -n "$script"
  grep -F -- '--runtime' "$script"
  grep -F -- '--r1s-bootstrap-destinations' "$script"
  grep -F -- 'CREDENTIALS_DIRECTORY/r1s-cluster-key' "$script"
  grep -F -- 'CREDENTIALS_DIRECTORY/agent-environment' "$script"
  # Secret paths become systemd credentials, never inline values in argv.
  if grep -F -- '/run/keys/cluster' "$script"; then exit 1; fi
  touch "$out"
''
