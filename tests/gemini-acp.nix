{ pkgs }:
pkgs.runCommand "gemini-acp-fsless-check" {
  nativeBuildInputs = [ pkgs.nodejs ];
} ''
  node ${./gemini-acp-smoke.mjs} ${pkgs.lattice.gemini-acp-fsless}/bin/gemini-acp-fsless
  touch "$out"
''
