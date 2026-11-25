#!/usr/bin/env bash
set -euo pipefail

if [[ $(uname -s) != Darwin || $(uname -m) != arm64 || $# -gt 1 || ${1:-} == -* ]]; then
  echo "Usage on Apple Silicon: $0 [homelab-ssh-destination]" >&2
  exit 2
fi

cd "$(dirname "$0")/.."
runtime_dir="$HOME/Library/Application Support/lattice/llama-embeddings"
agents_dir="$HOME/Library/LaunchAgents"
domain="gui/$(id -u)"
launchctl print "$domain" >/dev/null
umask 077
mkdir -p "$runtime_dir/cache" "$agents_dir"
nix build .#llama-embeddings --out-link "$runtime_dir/current"

python3 - "$runtime_dir" "$agents_dir" "$domain" "${1:-}" <<'PY'
import os
from pathlib import Path
import plistlib
import subprocess
import sys

runtime, agents = map(Path, sys.argv[1:3])
domain, homelab = sys.argv[3:]


def install(label, program):
    path = agents / f"{label}.plist"
    temporary = path.with_suffix(".tmp")
    with temporary.open("wb") as output:
        plistlib.dump({
            "Label": label,
            "ProgramArguments": program,
            "EnvironmentVariables": {"LLAMA_CACHE": str(runtime / "cache")},
            "WorkingDirectory": str(runtime),
            "RunAtLoad": True,
            "KeepAlive": True,
            "ThrottleInterval": 10,
            "StandardOutPath": str(runtime / f"{label}.log"),
            "StandardErrorPath": str(runtime / f"{label}.log"),
        }, output)
    os.replace(temporary, path)

    service = f"{domain}/{label}"
    installed = subprocess.run(
        ["launchctl", "print", service],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    ).returncode == 0
    if installed:
        subprocess.run(["launchctl", "bootout", service], check=True)
    subprocess.run(["launchctl", "bootstrap", domain, path], check=True)


install("local.lattice.llama-embeddings", [str(runtime / "current/bin/llama-embeddings")])
if homelab:
    install("local.lattice.llama-embeddings-tunnel", [
        "/usr/bin/ssh", "-NT",
        "-o", "BatchMode=yes",
        "-o", "StrictHostKeyChecking=yes",
        "-o", "ConnectTimeout=10",
        "-o", "ExitOnForwardFailure=yes",
        "-o", "ServerAliveInterval=30",
        "-o", "ServerAliveCountMax=3",
        "-R", "127.0.0.1:9227:127.0.0.1:9226",
        homelab,
    ])
PY
