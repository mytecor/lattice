{ lib, ... }:

let
  latticePorts = import ../networking/ports.nix;
in
{
  config = {
    services.radicle = {
      enable = true;
      publicKey = lib.mkDefault "dummy-key-for-vm";
      node = {
        listenPort = latticePorts.radicle-node;
      };
      httpd = {
        enable = true;
        listenPort = latticePorts.radicle-httpd;
      };
    };
  };
}
