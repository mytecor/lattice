{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.ephemeral-root;
  device = lib.escapeShellArg cfg.device;
  subvolumePath = lib.escapeShellArg "/mnt/${cfg.subvolume}";
in
{
  boot.initrd.supportedFilesystems = [ "btrfs" ];

  boot.initrd.systemd.services.wipe-root = {
    description = "Wipe root subvolume";
    wantedBy = [ "initrd.target" ];
    after = [ "initrd-root-device.target" ];
    before = [ "sysroot.mount" ];
    path = [ pkgs.btrfs-progs pkgs.util-linux ];
    serviceConfig = {
      Type = "oneshot";
      ExecStart = pkgs.writeShellScript "wipe-root" ''
        set -e
        mkdir -p /mnt
        mount -o subvol=/ ${device} /mnt
        trap 'umount /mnt' EXIT

        if [ -d ${subvolumePath} ]; then
          btrfs subvolume delete ${subvolumePath}
        fi
        btrfs subvolume create ${subvolumePath}
      '';
    };
  };

  environment.persistence."/persist" = {
    hideMounts = true;
    directories = [
      "/var/log"
      "/var/lib/nixos"
    ];
    files = [
      "/etc/machine-id"
    ];
  };

  environment.persistence."/data".hideMounts = true;
  environment.persistence."/var/cache".hideMounts = true;
}
