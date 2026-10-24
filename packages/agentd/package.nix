{ lib, buildGo127Module, runCommand, makeWrapper, git }:

let
  src = runCommand "lattice-agentd-src" { } ''
    mkdir -p $out/agentd $out/task-contract
    cp -r ${./.}/* $out/agentd/
    cp -r ${../task-contract}/* $out/task-contract/
  '';
in
buildGo127Module {
  pname = "lattice-agentd";
  version = "0.3.0";

  inherit src;
  modRoot = "agentd";
  vendorHash = "sha256-nJ1ZXmEHQtRV6xXWZcP9FlFjOpQvDR9UrZs6ewB6FI8=";

  nativeBuildInputs = [ makeWrapper ];
  nativeCheckInputs = [ git ];

  postInstall = ''
    wrapProgram $out/bin/agentd \
      --prefix PATH : ${lib.makeBinPath [ git ]}
  '';

  ldflags = [ "-s" "-w" "-X main.version=0.3.0" ];

  meta = {
    description = "Lattice agent execution daemon";
    homepage = "https://github.com/mytecor/lattice";
    license = lib.licenses.mit;
    mainProgram = "agentd";
    platforms = lib.platforms.unix;
  };
}
