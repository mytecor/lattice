{ lib, ... }:

{
  options.lattice.ephemeral-root = {
    subvolume = lib.mkOption {
      type = lib.types.str;
      example = "@root";
      description = "Btrfs root subvolume to recreate during initrd boot.";
    };

    device = lib.mkOption {
      type = lib.types.str;
      description = "Device mounted to access the top-level Btrfs volume.";
    };
  };
}
