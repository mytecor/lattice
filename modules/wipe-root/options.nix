{ lib, ... }:

{
  options.lattice.wipe-root = {
    subvolume = lib.mkOption {
      type = lib.types.str;
      example = "@root";
      description = "Btrfs subvolume to recreate during initrd boot.";
    };

    device = lib.mkOption {
      type = lib.types.str;
      default = "/dev/disk/by-label/root";
      description = "Device mounted to access the top-level Btrfs volume.";
    };
  };
}
