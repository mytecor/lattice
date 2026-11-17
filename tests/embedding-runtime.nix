{ pkgs, nixpkgs, runtimeModule }:
let
  stub = pkgs.writeShellScriptBin "embedding-runtime" "exit 0";
  evaluate = extra: (nixpkgs.lib.nixosSystem {
    system = "x86_64-linux";
    modules = [ runtimeModule {
      system.stateVersion = "26.05";
      lattice.embedding-runtime = {
        enable = true;
        package = stub;
        modelPath = "/model-fixture";
      };
    } extra ];
  }).config;
  cfg = evaluate { };
  unit = cfg.systemd.services.embedding-runtime;
  invalid = evaluate { lattice.embedding-runtime.idleSeconds = 0; };
  python = pkgs.python3.withPackages (ps: [ ps.aiohttp ]);
in
assert unit.serviceConfig.DynamicUser;
assert unit.serviceConfig.KillMode == "control-group";
assert unit.environment.HF_HUB_OFFLINE == "1";
assert nixpkgs.lib.hasInfix "--model-path" unit.serviceConfig.ExecStart;
assert !(builtins.elem cfg.lattice.embedding-runtime.port cfg.networking.firewall.allowedTCPPorts);
assert !(builtins.tryEval invalid.lattice.embedding-runtime.idleSeconds).success;
pkgs.runCommand "embedding-runtime-contract-and-smoke" { nativeBuildInputs = [ python ]; } ''
  export PYTHONPATH=${../packages/embedding-runtime}
  python -m unittest discover -s ${../packages/embedding-runtime/tests} -v
  touch $out
''
