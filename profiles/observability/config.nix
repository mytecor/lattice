{ lib, ... }:

let
  latticePorts = import ../networking/ports.nix;
in
{
  # F12 observability stack on the node: Prometheus (pull), Loki + Alloy
  # (journald push), Grafana frontend. All loopback-only and non-public.
  lattice.observability-prometheus = {
    enable = lib.mkDefault true;
    listenAddress = lib.mkDefault "127.0.0.1";
    port = lib.mkDefault latticePorts.prometheus;
  };

  lattice.observability-loki = {
    enable = lib.mkDefault true;
    listenAddress = lib.mkDefault "127.0.0.1";
    port = lib.mkDefault latticePorts.loki;
  };

  lattice.observability-alloy = {
    enable = lib.mkDefault true;
    listenAddress = lib.mkDefault "127.0.0.1";
    port = lib.mkDefault latticePorts.alloy;
    lokiUrl = lib.mkDefault "http://127.0.0.1:${toString latticePorts.loki}";
  };

  lattice.grafana = {
    enable = lib.mkDefault true;
    listenAddress = lib.mkDefault "127.0.0.1";
    port = lib.mkDefault latticePorts.grafana;
    prometheusUrl = lib.mkDefault "http://127.0.0.1:${toString latticePorts.prometheus}";
    lokiUrl = lib.mkDefault "http://127.0.0.1:${toString latticePorts.loki}";
  };
}
