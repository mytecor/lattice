{ pkgs, tokenProxy }:

pkgs.runCommand "token-proxy-spike" {
  nativeBuildInputs = [ pkgs.python3 tokenProxy ];
} ''
  python ${./token-proxy-spike.py} --token-proxy ${tokenProxy}/bin/token-proxy
  touch $out
''
