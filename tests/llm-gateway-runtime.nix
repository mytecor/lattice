{ pkgs }:

pkgs.runCommand "llm-gateway-packaged-runtime" {
  nativeBuildInputs = [ pkgs.lattice.llm-gateway pkgs.python3 ];
} ''
  python3 ${./llm-gateway-runtime.py} "$(command -v llm-gateway)"
  touch "$out"
''
