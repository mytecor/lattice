{ lib, ... }:

let
  latticePorts = import ../networking/ports.nix;
in
{
  lattice.llm-gateway = {
    enable = lib.mkDefault true;
    host = lib.mkDefault "127.0.0.1";
    port = lib.mkDefault latticePorts.llm-gateway;
    logLevel = lib.mkDefault "silent";
  };
}
