{ lib, ... }:

{
  lattice.ephemeral-root = {
    enable = true;
    subvolume = "@root";
    device = "/dev/disk/by-label/root";
    oldRootsDirectory = "@old-roots";
    retainedRoots = 1;
    failureMode = "continue";
  };

  disko.devices.disk.primary = {
    device = "/dev/disk/by-id/ata-EAGET_SSD_512GB_EAGET20250114W00252";
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
            extraArgs = [ "-n" "ESP" ];
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

  fileSystems."/".device = lib.mkForce "/dev/disk/by-label/root";
  fileSystems."/nix".device = lib.mkForce "/dev/disk/by-label/root";
  fileSystems."/persist".device = lib.mkForce "/dev/disk/by-label/root";
  fileSystems."/boot".device = lib.mkForce "/dev/disk/by-label/ESP";
  fileSystems."/persist".neededForBoot = true;
}
