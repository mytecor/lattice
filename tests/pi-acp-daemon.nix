{ nixpkgs, pkgs, acpModule, gatewayProfile }:

let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = [
      acpModule
      gatewayProfile
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "node-a";
        system.stateVersion = "26.05";

        lattice.pi-acp-daemon = {
          enable = true;
          # Transformers are wired into the generated Hydra config (shape-only
          # check in jq below; the daemon itself is not run by this test).
          transformers.fake-normalizer = {
            command = [ "/bin/echo" "fake-normalizer" ];
            enabled = true;
          };
          defaultTransformers = [ "fake-normalizer" ];
        };
      }
    ];
  }).config;

  cfg = config.lattice.pi-acp-daemon;
  unit = config.systemd.services.pi-acp-daemon;
  site = config.services.caddy.virtualHosts."http://acp.node-a.local";
  execStart = unit.serviceConfig.ExecStart;
in
assert cfg.host == "127.0.0.1";
assert cfg.port == 55514;
assert cfg.sessionIdleTimeoutSeconds == 3600;
assert lib.hasInfix "hydra-acp-daemon" execStart;
assert unit.serviceConfig.StateDirectory == "hydra-acp";
assert unit.environment.HYDRA_ACP_HOME == "/var/lib/hydra-acp";
assert config.services.caddy.environmentFile == null;
assert lib.hasInfix "rewrite * /acp?token=lattice-loopback-no-auth" site.extraConfig;
assert lib.hasInfix "header_up -Authorization" site.extraConfig;
assert lib.hasInfix "127.0.0.1:55514" site.extraConfig;
assert config.networking.firewall.allowedTCPPorts == [ 80 ];
assert builtins.hasAttr "acp-mdns" config.systemd.services;
pkgs.runCommand "pi-acp-daemon-evaluation" {
  nativeBuildInputs = [ pkgs.caddy pkgs.jq pkgs.nodejs ];
} ''
  mkdir -p "$out"

  jq -e '
    .daemon.host == "127.0.0.1" and
    .daemon.port == 55514 and
    .daemon.sessionIdleTimeoutSeconds == 3600 and
    .daemon.nonInteractiveOrphanTimeoutSeconds == 0 and
    .registry.pinned == true and
    .defaultAgent == "pi-acp" and
    .agents["pi-acp"].command == "${lib.getExe pkgs.lattice.pi-acp}" and
    .daemon.scrubEnv == [] and
    .defaultTransformers == ["fake-normalizer"] and
    .transformers["fake-normalizer"].command == ["/bin/echo", "fake-normalizer"] and
    .transformers["fake-normalizer"].enabled == true
  ' ${cfg.generatedConfigFile} > "$out/hydra-config.json"

  export XDG_DATA_HOME="$TMPDIR/caddy-data"
  export XDG_CONFIG_HOME="$TMPDIR/caddy-config"
  caddy adapt \
    --config ${config.services.caddy.configFile} \
    --adapter caddyfile \
    --validate > "$out/caddy-config.json"

  node ${./pi-acp-smoke.mjs} ${lib.getExe pkgs.lattice.pi-acp}
  node ${./acp-ingress-smoke.mjs} \
    ${lib.getExe' pkgs.lattice.hydra-acp "hydra-acp-daemon"} \
    ${lib.getExe pkgs.nodejs} \
    ${./fixtures/fake-acp-agent.mjs}
  node ${./hydra-acp-smoke.mjs} \
    ${lib.getExe' pkgs.lattice.hydra-acp "hydra-acp-daemon"} \
    ${lib.getExe pkgs.nodejs} \
    ${./fixtures/fake-acp-agent.mjs}
''
