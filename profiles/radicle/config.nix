{ latticePorts, lib, ... }:

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
