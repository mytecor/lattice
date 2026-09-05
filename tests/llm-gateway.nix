{ nixpkgs, pkgs, gatewayModule, gatewayProfile }:

let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = [
      gatewayModule
      gatewayProfile
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.llm-gateway = {
          package = pkgs.lattice.token-proxy;
          routing = {
            dispatch = "hedged";
            hedgeDelayMs = 250;
            maxParallel = 2;
          };
          upstreams.primary = {
            providers = [ "openai" "openai-response" ];
            baseUrl = "https://provider.invalid/v1";
            apiKeyFiles = [ "/run/agenix/llm-provider-primary-key" ];
            priority = 100;
            # Raw upstream model IDs exposed at the client boundary (no logical layer).
            availableModels = [ "model-fast" "model-standard" "model-strong" ];
          };
        };
      }
    ];
  }).config;

  service = config.systemd.services.llm-gateway;
  credentials = service.serviceConfig.LoadCredential;
in
assert config.lattice.llm-gateway.enable;
assert config.lattice.llm-gateway.host == "127.0.0.1";
assert config.lattice.llm-gateway.modelListPrefix == false;
assert config.lattice.llm-gateway.logicalModels == [ ];
assert builtins.elem
  "upstream-primary-0:/run/agenix/llm-provider-primary-key"
  credentials;
assert !(builtins.any (c: lib.hasPrefix "client-key:" c) credentials);
assert lib.hasInfix ".credential.api_keys" service.preStart;
assert !(lib.hasInfix "$CREDENTIALS_DIRECTORY/client-key" service.preStart);
assert lib.hasInfix "--config /run/llm-gateway/config.jsonc serve"
  service.serviceConfig.ExecStart;
assert service.serviceConfig.DynamicUser or false == false;
assert service.serviceConfig.NoNewPrivileges;
assert service.serviceConfig.ProtectSystem == "strict";
assert service.serviceConfig.PrivateDevices;
assert config.networking.firewall.allowedTCPPorts == [ ];
pkgs.runCommand "llm-gateway-module-evaluation" { } ''
  if grep -q 'model-fast\|model-standard\|model-strong' ${config.lattice.llm-gateway.publicConfigFile}; then
    :
  else
    echo "public routing template is missing advertised raw models" >&2
    exit 1
  fi

  if grep -q 'spike-client-key\|provider-primary-key' ${config.lattice.llm-gateway.publicConfigFile}; then
    echo "public routing template contains a secret marker" >&2
    exit 1
  fi

  if ! grep -q '"local_api_key":null' ${config.lattice.llm-gateway.publicConfigFile}; then
    echo "open gateway must keep local_api_key null" >&2
    exit 1
  fi

  touch "$out"
''
