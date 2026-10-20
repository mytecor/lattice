{ pkgs }:
pkgs.runCommand "muse-acp-fsless-check" {
  nativeBuildInputs = [ pkgs.nodejs ];
} ''
  node ${./muse-acp-smoke.mjs} ${pkgs.lattice.muse-acp-fsless}/bin/muse-acp-fsless tools
  node ${./muse-acp-smoke.mjs} ${pkgs.lattice.muse-acp-fsless}/bin/muse-acp-fsless no-tools
  touch "$out"
''
