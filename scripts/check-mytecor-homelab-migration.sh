#!/usr/bin/env bash
set -euo pipefail

flake_path=${1:-.}
age_key=${2:-.secrets/mytecor-homelab.agekey}
disk=/dev/disk/by-id/ata-EAGET_SSD_512GB_EAGET20250114W00252
boot_partition=/dev/sda2
root_partition=/dev/sda3
configuration=mytecor-homelab

fail() {
  printf 'Preflight failed: %s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null || fail "required command is missing: $1"
}

if [[ $EUID -ne 0 ]]; then
  fail "run this check as root on byurik"
fi

for command in awk blkid btrfs curl cut findmnt git grep ip nix nmcli readlink stat; do
  require_command "$command"
done

[[ -d /sys/firmware/efi ]] || fail "the current system is not booted through UEFI"
[[ -f "$age_key" ]] || fail "age identity is missing: $age_key"
[[ $(stat -c '%a' "$age_key") == 600 ]] || fail "age identity must have mode 0600"
[[ -f /etc/ssh/ssh_host_ed25519_key ]] || fail "the current SSH Ed25519 host key is missing"
[[ $(readlink -f "$disk") == /dev/sda ]] || fail "the expected EAGET SSD does not resolve to /dev/sda"
[[ $(readlink -f "$(findmnt -no SOURCE /)") == "$root_partition" ]] \
  || fail "the live root is not on $root_partition"
[[ $(readlink -f "$(findmnt -no SOURCE /boot)") == "$boot_partition" ]] \
  || fail "the live ESP is not on $boot_partition"
[[ $(blkid -s TYPE -o value "$root_partition") == btrfs ]] || fail "$root_partition is not Btrfs"
[[ $(blkid -s TYPE -o value "$boot_partition") == vfat ]] || fail "$boot_partition is not vfat"
findmnt -no OPTIONS / | grep -Eq '(^|,)subvolid=5(,|$)' \
  || fail "the live root must be the Btrfs top-level subvolume"

existing_root_label=$(blkid -L root 2>/dev/null || true)
existing_esp_label=$(blkid -L ESP 2>/dev/null || true)
[[ -z "$existing_root_label" || $(readlink -f "$existing_root_label") == "$root_partition" ]] \
  || fail "the root filesystem label is already used by another device"
[[ -z "$existing_esp_label" || $(readlink -f "$existing_esp_label") == "$boot_partition" ]] \
  || fail "the ESP filesystem label is already used by another device"

for subvolume in /@root /@nix /@persist; do
  [[ ! -e "$subvolume" ]] || fail "target path already exists: $subvolume"
done

nmcli -t -f TYPE,STATE device status | grep -qx 'wifi:connected' \
  || fail "Wi-Fi is not connected"
ip route show default | grep -q '^default ' || fail "the default route is missing"
curl --fail --silent --show-error --head --max-time 10 https://github.com/ >/dev/null \
  || fail "GitHub is not reachable"

git -C "$flake_path" rev-parse --is-inside-work-tree >/dev/null 2>&1 \
  || fail "$flake_path is not a Git checkout"
[[ -z $(git -C "$flake_path" status --porcelain) ]] \
  || fail "the Lattice checkout must be clean"

local_head=$(git -C "$flake_path" rev-parse HEAD)
remote_head=$(git ls-remote https://github.com/mytecor/lattice.git refs/heads/main | awk '{print $1}')
[[ -n "$remote_head" && "$local_head" == "$remote_head" ]] \
  || fail "HEAD must be published as GitHub main before migration"

system=$(nix build \
  --no-link \
  --print-out-paths \
  "$flake_path#nixosConfigurations.$configuration.config.system.build.toplevel")

age_bin=$(nix eval \
  --raw \
  "$flake_path#nixosConfigurations.$configuration.config.age.ageBin")

for secret in wifi-ssid wifi-password; do
  secret_path="$flake_path/nodes/$configuration/secrets/$secret.age"
  [[ -s "$secret_path" ]] || fail "encrypted secret is missing: $secret_path"
  value=$("$age_bin" --decrypt -i "$age_key" "$secret_path") \
    || fail "cannot decrypt $secret"
  [[ -n "$value" ]] || fail "$secret decrypts to an empty value"
  unset value
done

printf 'Preflight passed\n'
printf 'configuration=%s\n' "$configuration"
printf 'system=%s\n' "$system"
printf 'disk=%s\n' "$(readlink -f "$disk")"
printf 'network=%s\n' "$(nmcli -t -f GENERAL.CONNECTION device show wlp2s0 | cut -d: -f2-)"
