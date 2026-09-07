{ pkgs, syncPackage }:

pkgs.runCommand "comin-source-sync-test" {
  nativeBuildInputs = [ pkgs.git pkgs.jq syncPackage ];
} ''
  export HOME="$TMPDIR/home"
  mkdir -p "$HOME" "$TMPDIR/work"
  git config --global user.name test
  git config --global user.email test@localhost

  git init --bare "$TMPDIR/radicle"
  git init --bare "$TMPDIR/origin"
  git init "$TMPDIR/work"
  git -C "$TMPDIR/work" checkout -b main
  echo base > "$TMPDIR/work/value"
  git -C "$TMPDIR/work" add value
  git -C "$TMPDIR/work" commit -m base
  base=$(git -C "$TMPDIR/work" rev-parse HEAD)
  echo one > "$TMPDIR/work/value"
  git -C "$TMPDIR/work" add value
  git -C "$TMPDIR/work" commit -m one
  git -C "$TMPDIR/work" remote add radicle "$TMPDIR/radicle"
  git -C "$TMPDIR/work" remote add origin "$TMPDIR/origin"
  git -C "$TMPDIR/work" push radicle main
  git -C "$TMPDIR/work" push origin main

  export LATTICE_GITOPS_STATE_DIR="$TMPDIR/state"
  export LATTICE_GITOPS_RADICLE_REMOTE="$TMPDIR/radicle"
  export LATTICE_GITOPS_ORIGIN_REMOTE="$TMPDIR/origin"
  export LATTICE_GITOPS_BRANCH=main
  export LATTICE_GITOPS_COMIN_STATE_DIR="$TMPDIR/comin"

  lattice-comin-source-sync
  first=$(git -C "$TMPDIR/state/repository" rev-parse main)
  test "$first" = "$(git -C "$TMPDIR/work" rev-parse HEAD)"

  git -C "$TMPDIR/work" reset --hard HEAD~1
  echo rewritten > "$TMPDIR/work/value"
  git -C "$TMPDIR/work" add value
  git -C "$TMPDIR/work" commit -m rewritten
  rewritten=$(git -C "$TMPDIR/work" rev-parse HEAD)
  git -C "$TMPDIR/work" push --force radicle main
  git -C "$TMPDIR/work" push --force origin main

  mkdir -p "$TMPDIR/comin"
  git init --bare "$TMPDIR/comin/repository"
  git -C "$TMPDIR/comin/repository" fetch "$TMPDIR/work" \
    "$first:refs/heads/deployed"
  jq -n --arg baseline "$first" '{
    deployments: [{
      status: "done",
      generation: {source: {git: {main_commit_id: $baseline}}}
    }]
  }' > "$TMPDIR/comin/store.json"

  export LATTICE_GITOPS_STATE_DIR="$TMPDIR/bootstrap-state"
  lattice-comin-source-sync
  bootstrap_normalized=$(git -C "$TMPDIR/bootstrap-state/repository" rev-parse main)
  git -C "$TMPDIR/bootstrap-state/repository" merge-base --is-ancestor \
    "$first" "$bootstrap_normalized"
  test "$(git -C "$TMPDIR/bootstrap-state/repository" rev-parse "$bootstrap_normalized^{tree}")" = \
    "$(git -C "$TMPDIR/bootstrap-state/repository" rev-parse "$rewritten^{tree}")"

  export LATTICE_GITOPS_STATE_DIR="$TMPDIR/state"
  lattice-comin-source-sync
  normalized=$(git -C "$TMPDIR/state/repository" rev-parse main)
  test "$normalized" != "$rewritten"
  git -C "$TMPDIR/state/repository" merge-base --is-ancestor "$first" "$normalized"
  test "$(git -C "$TMPDIR/state/repository" rev-parse "$normalized^{tree}")" = \
    "$(git -C "$TMPDIR/state/repository" rev-parse "$rewritten^{tree}")"

  lattice-comin-source-sync
  test "$(git -C "$TMPDIR/state/repository" rev-parse main)" = "$normalized"

  echo origin-only > "$TMPDIR/work/value"
  git -C "$TMPDIR/work" add value
  git -C "$TMPDIR/work" commit -m origin-only
  git -C "$TMPDIR/work" push origin main
  lattice-comin-source-sync
  origin_ahead=$(git -C "$TMPDIR/work" rev-parse HEAD)
  test "$(git -C "$TMPDIR/state/repository" rev-parse refs/lattice/source)" = "$origin_ahead"

  git -C "$TMPDIR/work" reset --hard "$base"
  echo origin-diverged > "$TMPDIR/work/value"
  git -C "$TMPDIR/work" add value
  git -C "$TMPDIR/work" commit -m origin-diverged
  git -C "$TMPDIR/work" push --force origin main
  lattice-comin-source-sync
  test "$(git -C "$TMPDIR/state/repository" rev-parse refs/lattice/source)" = "$rewritten"

  mkdir "$out"
''
