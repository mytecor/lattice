{ pkgs }:
pkgs.runCommand "antigravity-acp-fsless-check" {
  nativeBuildInputs = [ pkgs.nodejs ];
} ''
  node ${./antigravity-acp-smoke.mjs} ${pkgs.lattice.antigravity-acp-fsless}/bin/antigravity-acp-fsless
  touch "$out"
''
