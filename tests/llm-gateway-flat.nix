{ nixpkgs, pkgs, gatewayModule }:

let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = [
      gatewayModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.llm-gateway = {
          enable = true;
          settings = {
            host = "127.0.0.1";
            port = 9208;
            providers = [{
              id = "provider-a";
              base_provider = "openai";
              inference_url = "https://a.invalid/v1";
            }];
            routing_rules = [
              { route = "standard"; action = "filter"; where.model.eq = "standard"; }
              { route = "standard"; action = "filter"; where.provider."in" = [ "provider-a" ]; }
              { route = "standard"; action = "map"; native = "native-model"; }
              { route = "standard"; action = "rank"; strategy = "priority"; }
              { route = "standard"; action = "race"; count = 1; }
            ];
          };
        };
      }
    ];
  }).config;

  staleConfig = lib.nixosSystem {
    modules = [
      gatewayModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.llm-gateway.enable = true;
        lattice.llm-gateway.routingRules = [ ];
      }
    ];
  };
in
assert builtins.tryEval staleConfig.config.lattice.llm-gateway.routingRules == {
  success = false;
  value = false;
};

pkgs.runCommand "llm-gateway-flat-settings" { nativeBuildInputs = [ pkgs.jq ]; } ''
  cfg=${config.lattice.llm-gateway.publicConfigFile}

  # The NixOS boundary is deliberately transparent: native snake_case fields
  # reach the gateway unchanged and no legacy Nix DSL keys are generated.
  jq -e '
    .providers == [{
      "id": "provider-a",
      "base_provider": "openai",
      "inference_url": "https://a.invalid/v1"
    }]
    and (.routing_rules | type == "array")
    and any(.routing_rules[];
      .route == "standard" and .action == "map" and .native == "native-model")
    and (has("models") | not)
    and (has("pipeline") | not)
    and (has("routingRules") | not)
  ' "$cfg" >/dev/null

  touch "$out"
''
