set -euo pipefail

: "${LATTICE_GITOPS_STATE_DIR:?LATTICE_GITOPS_STATE_DIR is required}"
: "${LATTICE_GITOPS_RADICLE_REMOTE:?LATTICE_GITOPS_RADICLE_REMOTE is required}"
: "${LATTICE_GITOPS_ORIGIN_REMOTE:?LATTICE_GITOPS_ORIGIN_REMOTE is required}"

branch="${LATTICE_GITOPS_BRANCH:-main}"
repository="$LATTICE_GITOPS_STATE_DIR/repository"
comin_state_dir="${LATTICE_GITOPS_COMIN_STATE_DIR:-/var/lib/comin}"

mkdir -p "$LATTICE_GITOPS_STATE_DIR"
exec 9>"$LATTICE_GITOPS_STATE_DIR/sync.lock"
flock 9

if [ ! -d "$repository/objects" ]; then
  git init --bare "$repository"
  git -C "$repository" config user.name "Lattice GitOps"
  git -C "$repository" config user.email "gitops@localhost"
  git -C "$repository" config gc.auto 0
  git -C "$repository" config core.hooksPath /dev/null
fi

configure_remote() {
  local name="$1"
  local url="$2"

  if git -C "$repository" remote get-url "$name" >/dev/null 2>&1; then
    git -C "$repository" remote set-url "$name" "$url"
  else
    git -C "$repository" remote add "$name" "$url"
  fi
}

fetch_remote() {
  local name="$1"
  local url="$2"

  if [[ "$url" = /* && ! -d "$url/objects" ]]; then
    return 1
  fi

  if ! timeout 5m git -c "safe.directory=$url" -C "$repository" fetch \
    --no-tags --force "$name" \
    "+refs/heads/$branch:refs/remotes/$name/$branch"; then
    echo "Failed to fetch $name from $url" >&2
    return 1
  fi

  git -C "$repository" rev-parse --verify "refs/remotes/$name/$branch"
}

configure_remote radicle "$LATTICE_GITOPS_RADICLE_REMOTE"
configure_remote origin "$LATTICE_GITOPS_ORIGIN_REMOTE"

radicle_head=""
origin_head=""
if fetched_head=$(fetch_remote radicle "$LATTICE_GITOPS_RADICLE_REMOTE"); then
  radicle_head="$fetched_head"
fi
if fetched_head=$(fetch_remote origin "$LATTICE_GITOPS_ORIGIN_REMOTE"); then
  origin_head="$fetched_head"
fi

selected_head=""
selected_remote=""
if [ -n "$radicle_head" ] && [ -n "$origin_head" ]; then
  if [ "$radicle_head" = "$origin_head" ]; then
    selected_head="$radicle_head"
    selected_remote="radicle"
  elif git -C "$repository" merge-base --is-ancestor "$radicle_head" "$origin_head"; then
    selected_head="$origin_head"
    selected_remote="origin"
  elif git -C "$repository" merge-base --is-ancestor "$origin_head" "$radicle_head"; then
    selected_head="$radicle_head"
    selected_remote="radicle"
  else
    # A disagreement after a force-push is resolved in favor of Radicle.
    selected_head="$radicle_head"
    selected_remote="radicle"
  fi
elif [ -n "$radicle_head" ]; then
  selected_head="$radicle_head"
  selected_remote="radicle"
elif [ -n "$origin_head" ]; then
  selected_head="$origin_head"
  selected_remote="origin"
else
  echo "Neither Radicle nor origin provides $branch" >&2
  exit 1
fi

normalized_ref="refs/heads/$branch"
source_ref="refs/lattice/source"
current_ref_head=$(git -C "$repository" rev-parse --verify "$normalized_ref" 2>/dev/null || true)
current_head="$current_ref_head"
previous_source=$(git -C "$repository" rev-parse --verify "$source_ref" 2>/dev/null || true)

if [ "$selected_head" = "$previous_source" ]; then
  exit 0
fi

if [ -z "$current_head" ] && [ -f "$comin_state_dir/store.json" ]; then
  baseline=$(
    jq -r '
      first(
        .deployments[]
        | select(.status == "done")
        | .generation.source.git.main_commit_id?
        | select(type == "string" and length == 40)
      ) // ""
    ' "$comin_state_dir/store.json"
  )
  if [ -n "$baseline" ]; then
    if ! timeout 1m git -c "safe.directory=$comin_state_dir/repository" \
      -C "$repository" fetch --no-tags "$comin_state_dir/repository" \
      "+$baseline:refs/lattice/bootstrap"; then
      echo "Failed to import comin baseline $baseline" >&2
      exit 1
    fi
    current_head="$baseline"
  fi
fi

if [ -z "$current_head" ] || [ "$current_head" = "$selected_head" ]; then
  normalized_head="$selected_head"
elif git -C "$repository" merge-base --is-ancestor "$current_head" "$selected_head"; then
  normalized_head="$selected_head"
else
  selected_tree=$(git -C "$repository" rev-parse "$selected_head^{tree}")
  normalized_head=$(
    printf 'Normalize non-fast-forward %s/%s at %s\n' \
      "$selected_remote" "$branch" "$selected_head" |
      git -C "$repository" commit-tree "$selected_tree" \
        -p "$current_head" -p "$selected_head"
  )
fi

git -C "$repository" update-ref "$normalized_ref" "$normalized_head" ${current_ref_head:+"$current_ref_head"}
git -C "$repository" update-ref "$source_ref" "$selected_head" ${previous_source:+"$previous_source"}

echo "Normalized $selected_remote/$branch at $selected_head as $normalized_head"
