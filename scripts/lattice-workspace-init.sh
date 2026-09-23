#!/usr/bin/env bash
# f15-01: idempotent bootstrap of the node's Lattice working checkout.
#
# Clones the Lattice RID from the local Radicle seed storage
# (LATTICE_WORKSPACE_RADICLE_REMOTE), falls back to GitHub when the seed
# storage does not yet have the objects, and sets up the `publish` remote
# (Radicle + GitHub push URLs) following the DEPLOYMENT.md recipe. The
# workspace is a real working copy (separate from /var/lib/comin source and
# /var/lib/radicle storage); sessions that start there (pi-acp-daemon
# defaultCwd, f15-01) can edit, commit and push `main` from the node itself.
#
# Idempotent: if the checkout already exists it is not disturbed (git pull
# --ff-only keeps it current; a dirty tree is left as-is), no symlinks are
# created in /var/lib/comin or /var/lib/radicle.

set -euo pipefail

: "${LATTICE_WORKSPACE_DIR:?LATTICE_WORKSPACE_DIR is required}"
: "${LATTICE_WORKSPACE_RADICLE_REMOTE:?LATTICE_WORKSPACE_RADICLE_REMOTE is required}"

workspace_dir="$LATTICE_WORKSPACE_DIR"
checkout="$workspace_dir/lattice"
branch="${LATTICE_WORKSPACE_BRANCH:-main}"
origin_remote="${LATTICE_WORKSPACE_ORIGIN_REMOTE:-https://github.com/mytecor/lattice.git}"
# Push URLs per DEPLOYMENT.md "Публикация в Radicle и GitHub".
radicle_push_url="${LATTICE_WORKSPACE_RADICLE_PUSH_URL:-rad://z3AqC22BKQ5Gnrkw49N7PGJa91G6L/z6Mkvq7AcVgfLmaecxQEasuErFk6s7fLDj2668WLBFCE9xWV}"
git_author_name="${LATTICE_WORKSPACE_AUTHOR_NAME:-Lattice Node Dev}"
git_author_email="${LATTICE_WORKSPACE_AUTHOR_EMAIL:-node-dev@localhost}"

umask 0077

mkdir -p "$workspace_dir"

if [ ! -d "$checkout/.git" ]; then
  echo "lattice-workspace-init: cloning $branch into $checkout"

  # Prefer the local Radicle seed storage; fall back to GitHub when the seed
  # storage has no objects yet (mirror of comin-source-sync's radicle-first
  # policy). The storage is owned by the `radicle` service user, so git needs
  # an explicit safe.directory for it (same as comin-source-sync.sh).
  radicle_objects="$LATTICE_WORKSPACE_RADICLE_REMOTE"/objects
  if [ -d "$radicle_objects" ]; then
    git -c "safe.directory=$LATTICE_WORKSPACE_RADICLE_REMOTE" \
      clone --branch "$branch" "$LATTICE_WORKSPACE_RADICLE_REMOTE" "$checkout"
  else
    echo "lattice-workspace-init: radicle storage empty, falling back to $origin_remote"
    git clone --branch "$branch" "$origin_remote" "$checkout"
  fi

  git -C "$checkout" config user.name "$git_author_name"
  git -C "$checkout" config user.email "$git_author_email"
fi

# The `publish` remote: GitHub as fetch URL, both push URLs appended
# (Radicle + GitHub). Only add push URLs that are not already present, so the
# script stays idempotent across reconfigurations.
git -C "$checkout" remote set-url origin "$origin_remote"

if ! push_urls=$(git -C "$checkout" remote get-url --all --push publish 2>/dev/null); then
  git -C "$checkout" remote add publish "$origin_remote"
fi

configure_push_url() {
  local url="$1"
  if ! printf '%s\n' "$push_urls" | grep -Fxq "$url"; then
    git -C "$checkout" remote set-url --add --push publish "$url"
  fi
}
configure_push_url "$radicle_push_url"
configure_push_url "$origin_remote"

# Keep the working copy on the latest main without clobbering a dirty tree.
# The persistence-managed workspace is a dev copy; if a session left changes,
# we must not overwrite them.
git -C "$checkout" fetch --ff-only origin "$branch" || true
if git -C "$checkout" diff --quiet HEAD; then
  git -C "$checkout" pull --ff-only origin "$branch" || true
else
  echo "lattice-workspace-init: dirty tree, leaving checkout untouched"
fi

echo "lattice-workspace-init: workspace ready at $checkout"
