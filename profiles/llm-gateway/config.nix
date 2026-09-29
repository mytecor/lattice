{ lib, ... }:

let
  latticePorts = import ../networking/ports.nix;
in
{
  lattice.llm-gateway = {
    enable = lib.mkDefault true;
    settings = {
      host = lib.mkDefault "127.0.0.1";
      port = lib.mkDefault latticePorts.llm-gateway;
      metrics_host = lib.mkDefault "127.0.0.1";
      metrics_port = lib.mkDefault 9209;
      log_level = lib.mkDefault "silent";
      catalog_refresh_interval = lib.mkDefault "10m";
      stream_idle_timeout = lib.mkDefault "5m";
      affinity_file = lib.mkDefault "/run/llm-gateway/affinity.json";
    };
  };
}
