{ lib, ... }:

let
  latticePorts = import ../networking/ports.nix;
in
{
  lattice.git-cache-proxy = {
    enable = lib.mkDefault true;
    host = lib.mkDefault "127.0.0.1";
    port = lib.mkDefault latticePorts.git-cache-proxy;
    upstream = lib.mkDefault "https://github.com";
    cacheRoot = lib.mkDefault "/var/cache/git-cache-proxy";
  };
}
