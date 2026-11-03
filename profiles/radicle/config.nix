{ config, lib, pkgs, ... }:

let
  latticePorts = import ../networking/ports.nix;
  latticeRepository = (import ./repositories.nix).lattice;
  dbGuard = lib.getExe pkgs.lattice.radicle-db-guard;
  radicleHome = "/var/lib/radicle";
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

    # Guard against malformed gossip announcements where a bracketed IPv6
    # literal is stored with type='dns', which leaves last_attempt=NULL and
    # spins radicle-node at 100% CPU. `+` runs outside the unit's chroot
    # confinement so sqlite can access /var/lib/radicle/node/node.db directly.
    systemd.services.radicle-node.serviceConfig = {
      ExecStartPre = [ "+${dbGuard} ${radicleHome}" ];
      ExecStartPost = [ "+${dbGuard} --wait ${radicleHome}" ];
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
