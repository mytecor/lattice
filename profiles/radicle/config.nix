{ config, ... }:

let
  latticePorts = import ../networking/ports.nix;
  latticeRepository = (import ./repositories.nix).lattice;
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
      settings = {
        node.alias = config.networking.hostName;
        node.seedingPolicy.default = "block";
        web.pinned.repositories = [ latticeRepository.rid ];
      };
      httpd = {
        enable = true;
        listenPort = latticePorts.radicle-httpd;
        aliases.lattice = latticeRepository.rid;
      };
    };

    # Persist the selective seed policy and fetch the repository from any
    # connected Radicle seed. A failed first attempt is retried while routing
    # information and repository inventory propagate through the network.
    systemd.services.radicle-seed-lattice = {
      description = "Bootstrap the Lattice repository into Radicle storage";
      after = [ "radicle-node.service" ];
      requires = [ "radicle-node.service" ];
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        Type = "oneshot";
        ExecStart =
          "/run/current-system/sw/bin/rad-system seed --scope followed --timeout 5min ${latticeRepository.rid}";
        RemainAfterExit = true;
        Restart = "on-failure";
        RestartSec = "1min";
        TimeoutStartSec = "6min";
      };
    };
  };
}
