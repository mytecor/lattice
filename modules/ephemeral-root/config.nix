{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.ephemeral-root;
  rootDevice = lib.attrByPath [ "fileSystems" "/" "device" ] null config;
  rootFsType = lib.attrByPath [ "fileSystems" "/" "fsType" ] null config;
  rootMountOptions = lib.attrByPath [ "fileSystems" "/" "options" ] [ ] config;
  persistNeededForBoot = lib.attrByPath [ "fileSystems" "/persist" "neededForBoot" ] false config;
  device = if cfg.device == null then rootDevice else cfg.device;
  deviceForScript = if device == null then "/dev/null" else device;
  expectedSubvolumeOptions = [ "subvol=${cfg.subvolume}" "subvol=/${cfg.subvolume}" ];
  rootUsesConfiguredSubvolume = lib.any (option: builtins.elem option expectedSubvolumeOptions) rootMountOptions;

  rotateRoot = pkgs.writeShellScript "lattice-ephemeral-root-rotate" ''
    set -eu

    if [ "$#" -ne 3 ]; then
      printf 'Usage: %s DEVICE ROOT_SUBVOLUME OLD_ROOTS_DIRECTORY\n' "$0" >&2
      exit 64
    fi

    device=$1
    root_name=$2
    old_roots_name=$3
    state_dir=/run/lattice-ephemeral-root
    top_level="$state_dir/top"
    root="$top_level/$root_name"
    old_roots="$top_level/$old_roots_name"
    retired=
    mounted=

    finish() {
      if [ -n "$mounted" ]; then
        umount "$top_level" || true
      fi
    }

    restore_root() {
      if [ -z "$retired" ]; then
        return 0
      fi

      if [ -e "$root" ]; then
        btrfs subvolume show "$root" >/dev/null || return 1
        btrfs subvolume delete --recursive --commit-after -- "$root" || return 1
      fi

      if [ -e "$retired" ]; then
        mv -- "$retired" "$root" || return 1
      fi
    }

    fail() {
      printf 'Ephemeral root rotation failed: %s\n' "$1" >&2
      if ! restore_root; then
        printf 'Failed to restore the retired root subvolume\n' >&2
      fi
      printf 'degraded\n' > "$state_dir/status"
      exit 1
    }

    trap finish EXIT
    mkdir -p "$top_level"

    mount -t btrfs -o rw,subvolid=5 "$device" "$top_level" \
      || fail "cannot mount the Btrfs top level"
    mounted=1

    mkdir -p "$old_roots"

    if [ -e "$root" ]; then
      btrfs subvolume show "$root" >/dev/null \
        || fail "$root_name exists but is not a Btrfs subvolume"

      boot_id=$(cat /proc/sys/kernel/random/boot_id)
      candidate="$old_roots/$boot_id"
      if [ -e "$candidate" ]; then
        fail "retired root $candidate already exists"
      fi

      mv -- "$root" "$candidate" || fail "cannot retire $root_name"
      retired=$candidate
      touch "$retired" || fail "cannot timestamp the retired $root_name"
    fi

    if ! btrfs subvolume create "$root"; then
      fail "cannot create a fresh $root_name"
    fi

    chmod 0755 "$root" || fail "cannot set permissions on the fresh $root_name"
    touch "$root/.lattice-ephemeral-root-ready" \
      || fail "cannot mark the fresh $root_name as ready"

    if ! btrfs filesystem sync "$top_level"; then
      fail "cannot commit the fresh $root_name"
    fi

    printf 'ready\n' > "$state_dir/status"
  '';

  pruneRoots = pkgs.writeShellScript "lattice-ephemeral-root-prune" ''
    set -euo pipefail

    if [ "$#" -ne 3 ]; then
      printf 'Usage: %s DEVICE OLD_ROOTS_DIRECTORY RETAINED_ROOTS\n' "$0" >&2
      exit 64
    fi

    device=$1
    old_roots_name=$2
    retain=$3
    ready_marker=''${LATTICE_EPHEMERAL_ROOT_READY_MARKER:-/.lattice-ephemeral-root-ready}
    top_level=/run/lattice-ephemeral-root-prune
    old_roots="$top_level/$old_roots_name"

    finish() {
      umount "$top_level" || true
    }

    trap finish EXIT
    mkdir -p "$top_level"
    mount -t btrfs -o rw,subvolid=5 "$device" "$top_level"

    if [ ! -d "$old_roots" ]; then
      rm -f "$ready_marker"
      exit 0
    fi

    mapfile -t candidates < <(
      find "$old_roots" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' \
        | sort -rn \
        | cut -d' ' -f2-
    )

    index=0
    for candidate in "''${candidates[@]}"; do
      index=$((index + 1))
      if [ "$index" -le "$retain" ]; then
        continue
      fi

      if ! btrfs subvolume show "$candidate" >/dev/null; then
        printf 'Skipping non-subvolume retired root: %s\n' "$candidate" >&2
        continue
      fi

      btrfs subvolume delete --recursive --commit-after -- "$candidate"
    done

    rm -f "$ready_marker"
  '';
in
{
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = device != null;
        message = "lattice.ephemeral-root requires a device or fileSystems.\"/\".device";
      }
      {
        assertion = rootFsType == "btrfs";
        message = "lattice.ephemeral-root requires fileSystems.\"/\".fsType = \"btrfs\"";
      }
      {
        assertion = rootUsesConfiguredSubvolume;
        message = "fileSystems.\"/\" must mount lattice.ephemeral-root.subvolume via subvol=<name>";
      }
      {
        assertion = persistNeededForBoot;
        message = "lattice.ephemeral-root requires fileSystems.\"/persist\".neededForBoot = true";
      }
      {
        assertion = cfg.subvolume != cfg.oldRootsDirectory;
        message = "lattice.ephemeral-root.subvolume and oldRootsDirectory must differ";
      }
      {
        assertion = lib.versionAtLeast pkgs.btrfs-progs.version "6.12";
        message = "lattice.ephemeral-root requires btrfs-progs 6.12 or newer";
      }
      {
        assertion = config.boot.initrd.systemd.enable;
        message = "lattice.ephemeral-root requires the systemd initrd";
      }
    ];

    boot.initrd.supportedFilesystems = [ "btrfs" ];
    boot.initrd.systemd.storePaths = [ rotateRoot ];

    boot.initrd.systemd.services.lattice-ephemeral-root = {
      description = "Rotate and recreate the Btrfs root subvolume";
      wantedBy = lib.optionals (cfg.failureMode == "continue") [ "sysroot.mount" ];
      requiredBy = lib.optionals (cfg.failureMode == "emergency") [ "sysroot.mount" ];
      after = [ "initrd-root-device.target" "systemd-hibernate-resume.service" ];
      before = [ "sysroot.mount" ];
      path = [ pkgs.btrfs-progs pkgs.coreutils pkgs.util-linux ];
      unitConfig.DefaultDependencies = false;
      serviceConfig = {
        Type = "oneshot";
        PrivateMounts = true;
        ExecStart = "${rotateRoot} ${lib.escapeShellArgs [ deviceForScript cfg.subvolume cfg.oldRootsDirectory ]}";
      };
    };

    systemd.services.lattice-ephemeral-root-prune = {
      description = "Prune retired Btrfs root subvolumes";
      wantedBy = [ "multi-user.target" ];
      after = [ "local-fs.target" ];
      path = [ pkgs.btrfs-progs pkgs.coreutils pkgs.findutils pkgs.util-linux ];
      unitConfig.ConditionPathExists = "/.lattice-ephemeral-root-ready";
      serviceConfig = {
        Type = "oneshot";
        PrivateMounts = true;
        ExecStart = "${pruneRoots} ${lib.escapeShellArgs [ deviceForScript cfg.oldRootsDirectory (toString cfg.retainedRoots) ]}";
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
  };
}
