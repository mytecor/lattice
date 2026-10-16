{ lib, buildGo127Module, makeWrapper, git }:

buildGo127Module {
  pname = "lattice-git-watchd";
  version = "0.1.0";

  src = ./.;
  vendorHash = null;

  nativeBuildInputs = [ makeWrapper ];
  nativeCheckInputs = [ git ];

  postInstall = ''
    wrapProgram $out/bin/git-watchd \
      --prefix PATH : ${lib.makeBinPath [ git ]}
  '';

  ldflags = [ "-s" "-w" "-X main.version=0.1.0" ];

  meta = {
    description = "Lattice Git revision watcher and Meshbus adapter";
    homepage = "https://github.com/mytecor/lattice";
    license = lib.licenses.mit;
    mainProgram = "git-watchd";
    platforms = lib.platforms.unix;
  };
}
