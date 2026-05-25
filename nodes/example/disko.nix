{ ... }:

{
  lattice.ephemeral-root.subvolume = "@root";
  lattice.ephemeral-root.device = "/dev/disk/by-label/root";

  disko.devices.disk.primary = {
    device = "/dev/disk/by-id/replace-me";
    type = "disk";
    content = {
      type = "gpt";
      partitions = {
        ESP = {
          type = "EF00";
          size = "512M";
          content = {
            type = "filesystem";
            format = "vfat";
            mountpoint = "/boot";
            mountOptions = [ "umask=0077" ];
          };
        };

        main = {
          size = "100%";
          content = {
            type = "btrfs";
            extraArgs = [ "-L" "root" ];
            subvolumes = {
              "@root" = {
                mountpoint = "/";
                mountOptions = [ "compress=zstd" "noatime" ];
              };

              "@nix" = {
                mountpoint = "/nix";
                mountOptions = [ "compress=zstd" "noatime" ];
              };

              "@persist" = {
                mountpoint = "/persist";
                mountOptions = [ "compress=zstd" ];
              };
            };
          };
        };
      };
    };
  };

  boot.loader.systemd-boot.enable = true;
  boot.loader.efi.canTouchEfiVariables = true;

  fileSystems."/persist".neededForBoot = true;

}
