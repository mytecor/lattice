{ config, ... }:

let
  latticePorts = import ../networking/ports.nix;
in
{
  config = {
    services.radicle = {
      enable = true;
      # The node supplies its public key and encrypted radicle-private-key secret.
      # Only the decrypted runtime path is passed to systemd LoadCredential.
      privateKey = config.age.secrets.radicle-private-key.path;
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
