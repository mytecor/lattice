{ lib, ... }:

let
  latticePorts = import ../networking/ports.nix;
in
{
  lattice.agentrun-openai = {
    enable = lib.mkDefault true;
    host = lib.mkDefault "127.0.0.1";
    port = lib.mkDefault latticePorts.agentrun-openai;
    # Session metadata persisted under /var/lib/agentrun-openai via
    # StateDirectory (impermanence via nodes' persist entry).
  };
}
