#!/usr/bin/env bash
set -euo pipefail

confirmation=${1:-}
flake_path=${2:-.}
age_key=${3:-.secrets/mytecor-homelab.agekey}
expected_confirmation=INSTALL-MYTECOR-HOMELAB-ON-EAGET20250114W00252
disk=/dev/disk/by-id/ata-EAGET_SSD_512GB_EAGET20250114W00252
boot_partition=/dev/sda2
root_partition=/dev/sda3
configuration=mytecor-homelab
target=/mnt/lattice-target
top_level=/mnt/lattice-top
boot_was_unmounted=

fail() {
  printf 'Install failed: %s\n' "$1" >&2
  exit 1
}

cleanup() {
  set +e

  for mount_point in "$target/boot" "$target/persist" "$target/nix" "$target" "$top_level"; do
    if mountpoint -q "$mount_point"; then
      umount "$mount_point"
    fi
  done

  if [[ -n "$boot_was_unmounted" ]] && ! mountpoint -q /boot; then
    mount "$boot_partition" /boot
  fi
}

trap cleanup EXIT

if [[ $EUID -ne 0 ]]; then
  fail "run this installer as root on byurik"
fi

if [[ "$confirmation" != "$expected_confirmation" ]]; then
  printf 'This command prepares a new bootable NixOS installation on the existing SSD.\n' >&2
  printf 'Re-run with confirmation: %s\n' "$expected_confirmation" >&2
  exit 64
fi

for command in btrfs cmp fatlabel grep install mount mountpoint nix nixos-install readlink sync udevadm umount; do
  command -v "$command" >/dev/null || fail "required command is missing: $command"
done

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
"$script_dir/check-mytecor-homelab-migration.sh" "$flake_path" "$age_key"

system=$(nix build \
  --no-link \
  --print-out-paths \
  "$flake_path#nixosConfigurations.$configuration.config.system.build.toplevel")

age_bin=$(nix eval \
  --raw \
  "$flake_path#nixosConfigurations.$configuration.config.age.ageBin")

[[ $(readlink -f "$disk") == /dev/sda ]] || fail "unexpected target disk"

mkdir -p "$target" "$top_level"
mount -t btrfs -o rw,subvolid=5 "$root_partition" "$top_level"

for subvolume in @root @nix @persist; do
  [[ ! -e "$top_level/$subvolume" ]] || fail "target subvolume already exists: $subvolume"
done

if mountpoint -q /boot; then
  umount /boot
  boot_was_unmounted=1
fi

fatlabel "$boot_partition" ESP
btrfs filesystem label "$top_level" root
udevadm trigger --subsystem-match=block
udevadm settle

[[ $(readlink -f /dev/disk/by-label/ESP) == "$boot_partition" ]] \
  || fail "ESP filesystem label was not applied"
[[ $(readlink -f /dev/disk/by-label/root) == "$root_partition" ]] \
  || fail "root filesystem label was not applied"

btrfs subvolume create "$top_level/@root"
btrfs subvolume create "$top_level/@nix"
btrfs subvolume create "$top_level/@persist"

mount -t btrfs -o rw,compress=zstd,noatime,subvol=@root /dev/disk/by-label/root "$target"
mkdir -p "$target/boot" "$target/nix" "$target/persist"
mount -t btrfs -o rw,compress=zstd,noatime,subvol=@nix /dev/disk/by-label/root "$target/nix"
mount -t btrfs -o rw,compress=zstd,subvol=@persist /dev/disk/by-label/root "$target/persist"
mount -t vfat -o umask=0077 /dev/disk/by-label/ESP "$target/boot"

install -D -m 0600 "$age_key" "$target/persist/var/lib/lattice/age/identity"
install -D -m 0600 /etc/ssh/ssh_host_ed25519_key \
  "$target/persist/etc/ssh/ssh_host_ed25519_key"
install -D -m 0644 /etc/ssh/ssh_host_ed25519_key.pub \
  "$target/persist/etc/ssh/ssh_host_ed25519_key.pub"

nixos-install \
  --root "$target" \
  --system "$system" \
  --no-root-password \
  --no-channel-copy

profile=$(readlink -f "$target/nix/var/nix/profiles/system")
[[ "$profile" == "$system" ]] || fail "the installed system profile does not match the built closure"

grep -RqsF "init=$system/init" "$target/boot/loader/entries" \
  || fail "no systemd-boot entry references the installed system"

cmp -s /etc/ssh/ssh_host_ed25519_key.pub \
  "$target/persist/etc/ssh/ssh_host_ed25519_key.pub" \
  || fail "the target SSH host key does not match the current host key"

secrets=(wifi-ssid wifi-password)
if [[ -e "$flake_path/nodes/$configuration/secrets/root-password-hash.age" ]]; then
  secrets+=(root-password-hash)
fi

for secret in "${secrets[@]}"; do
  "$age_bin" --decrypt \
    -i "$target/persist/var/lib/lattice/age/identity" \
    "$flake_path/nodes/$configuration/secrets/$secret.age" \
    >/dev/null \
    || fail "the installed identity cannot decrypt $secret"
done

sync

printf 'Installation completed without rebooting.\n'
printf 'Target system: %s\n' "$system"
printf 'Offline checks passed: profile, boot entry, secrets and SSH host key.\n'
printf 'Reboot remains a separate explicit action.\n'
