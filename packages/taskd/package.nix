{ lib, buildGo127Module, runCommand, makeWrapper, git }:

let
  src = runCommand "lattice-taskd-src" { } ''
    mkdir -p $out/taskd $out/task-contract
    cp -r ${./.}/* $out/taskd/
    cp -r ${../task-contract}/* $out/task-contract/
  '';
in
buildGo127Module {
  pname = "lattice-taskd";
  version = "0.1.0";

  inherit src;
  modRoot = "taskd";
  vendorHash = lib.fakeHash;

  nativeBuildInputs = [ makeWrapper ];
  nativeCheckInputs = [ git ];

  postInstall = ''
    wrapProgram $out/bin/taskd \
      --prefix PATH : ${lib.makeBinPath [ git ]}
  '';

  ldflags = [ "-s" "-w" "-X main.version=0.1.0" ];

  meta = {
    description = "Lattice Git task reconciler daemon";
    homepage = "https://github.com/mytecor/lattice";
    license = lib.licenses.mit;
    mainProgram = "taskd";
    platforms = lib.platforms.unix;
  };
}
