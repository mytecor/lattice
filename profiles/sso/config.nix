{ lib, config, ... }:

let
  latticePorts = import ../networking/ports.nix;
in
{
  # F14: central SSO (Authentik) behind the existing Caddy ingress. Loopback-only,
  # non-public by design. Secrets and domain are node-specific (see nodes/…).
  lattice.authentik = {
    enable = lib.mkDefault true;
    listenAddress = lib.mkDefault "127.0.0.1";
    port = lib.mkDefault latticePorts.authentik;
  };
}
