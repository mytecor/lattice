{ lib, ... }:

{
  options.lattice.ephemeral-root = {
    enable = lib.mkEnableOption "a Btrfs-backed ephemeral root";

    subvolume = lib.mkOption {
      type = lib.types.strMatching "[A-Za-z0-9@._+-]+";
      default = "@root";
      example = "@root";
      description = "Btrfs root subvolume to rotate and recreate during initrd boot.";
    };

    device = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "/dev/disk/by-label/root";
      description = "Device mounted to access the top-level Btrfs volume; defaults to the root filesystem device.";
    };

    oldRootsDirectory = lib.mkOption {
      type = lib.types.strMatching "[A-Za-z0-9@._+-]+";
      default = "@old-roots";
      description = "Top-level Btrfs directory used for retired root subvolumes.";
    };

    retainedRoots = lib.mkOption {
      type = lib.types.ints.unsigned;
      default = 1;
      description = "Number of retired root subvolumes retained after a successful boot.";
    };

    failureMode = lib.mkOption {
      type = lib.types.enum [ "continue" "emergency" ];
      default = "continue";
      description = ''
        Behaviour when root rotation fails. "continue" boots the current root when possible;
        "emergency" makes the root mount depend on successful rotation.
      '';
    };
  };
}
