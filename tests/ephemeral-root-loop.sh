#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  printf 'Usage: %s ROTATE_SCRIPT PRUNE_SCRIPT\n' "$0" >&2
  exit 64
fi

rotate_script=$1
prune_script=$2

for script in "$rotate_script" "$prune_script"; do
  if [[ ! -x "$script" ]]; then
    printf 'Not executable: %s\n' "$script" >&2
    exit 66
  fi
done

test_dir=$(mktemp -d /tmp/lattice-ephemeral-root-test.XXXXXX)
image="$test_dir/btrfs.img"
top_level="$test_dir/top"
loop_device=

cleanup() {
  set +e

  for mount_point in \
    /run/lattice-ephemeral-root/top \
    /run/lattice-ephemeral-root-prune \
    "$top_level"; do
    if mountpoint -q "$mount_point"; then
      umount "$mount_point"
    fi
  done

  if [[ -n "$loop_device" && "$loop_device" == /dev/loop* ]]; then
    losetup -d "$loop_device"
  fi

  rm -f /run/lattice-ephemeral-root/status
  rmdir /run/lattice-ephemeral-root/top /run/lattice-ephemeral-root 2>/dev/null || true
  rmdir /run/lattice-ephemeral-root-prune 2>/dev/null || true

  case "$test_dir" in
    /tmp/lattice-ephemeral-root-test.*)
      rm -rf -- "$test_dir"
      ;;
    *)
      printf 'Refusing to remove unexpected test directory: %s\n' "$test_dir" >&2
      ;;
  esac
}

trap cleanup EXIT

truncate -s 512M "$image"
mkfs.btrfs -q -f "$image"
loop_device=$(losetup --find --show "$image")

if [[ "$loop_device" != /dev/loop* ]]; then
  printf 'Unexpected loop device: %s\n' "$loop_device" >&2
  exit 1
fi

mkdir -p "$top_level"
mount -t btrfs -o rw,subvolid=5 "$loop_device" "$top_level"

btrfs subvolume create "$top_level/@root" >/dev/null
btrfs subvolume create "$top_level/@nix" >/dev/null
btrfs subvolume create "$top_level/@persist" >/dev/null
mkdir -p "$top_level/@root/var/lib"
btrfs subvolume create "$top_level/@root/var/lib/machines" >/dev/null
printf 'discard me\n' > "$top_level/@root/ephemeral-marker"
umount "$top_level"

"$rotate_script" "$loop_device" @root @old-roots

mount -t btrfs -o rw,subvolid=5 "$loop_device" "$top_level"
test -f "$top_level/@root/.lattice-ephemeral-root-ready"
test ! -e "$top_level/@root/ephemeral-marker"
test -d "$top_level/@nix"
test -d "$top_level/@persist"

mapfile -t retired_roots < <(find "$top_level/@old-roots" -mindepth 1 -maxdepth 1 -type d)
if [[ ${#retired_roots[@]} -ne 1 ]]; then
  printf 'Expected one retired root, found %s\n' "${#retired_roots[@]}" >&2
  exit 1
fi

test -f "${retired_roots[0]}/ephemeral-marker"
btrfs subvolume show "${retired_roots[0]}/var/lib/machines" >/dev/null

btrfs subvolume create "$top_level/@old-roots/older" >/dev/null
mkdir -p "$top_level/@old-roots/older/var/lib"
btrfs subvolume create "$top_level/@old-roots/older/var/lib/machines" >/dev/null
touch -t 200001010000 "$top_level/@old-roots/older"
umount "$top_level"

if "$rotate_script" "$loop_device" @root @old-roots; then
  printf 'A duplicate boot ID should make rotation fail safely\n' >&2
  exit 1
fi

mount -t btrfs -o rw,subvolid=5 "$loop_device" "$top_level"
btrfs subvolume show "$top_level/@root" >/dev/null
umount "$top_level"

ready_marker="$test_dir/prune-ready"
touch "$ready_marker"
LATTICE_EPHEMERAL_ROOT_READY_MARKER="$ready_marker" \
  "$prune_script" "$loop_device" @old-roots 1

test ! -e "$ready_marker"

mount -t btrfs -o rw,subvolid=5 "$loop_device" "$top_level"
mapfile -t retained_roots < <(find "$top_level/@old-roots" -mindepth 1 -maxdepth 1 -type d)
if [[ ${#retained_roots[@]} -ne 1 ]]; then
  printf 'Expected one retained root, found %s\n' "${#retained_roots[@]}" >&2
  exit 1
fi
test ! -e "$top_level/@old-roots/older"

printf 'ephemeral-root loopback test passed\n'
