{ pkgs }:
pkgs.runCommand "codex-acp-fsless-check" {
  nativeBuildInputs = [ pkgs.bash pkgs.coreutils pkgs.jq pkgs.nodejs ];
} ''
  bash ${./codex-auth-seed.sh} ${../scripts/seed-codex-auth.sh}
  node ${./codex-acp-smoke.mjs} ${pkgs.lattice.codex-acp-fsless}/bin/codex-acp-fsless
  touch "$out"
''
