{ lib, buildGo127Module }:

buildGo127Module {
  pname = "lattice-task-contract";
  version = "0.1.0";

  src = ./.;
  vendorHash = null;

  subPackages = [ "cmd/task-validate" ];

  ldflags = [ "-s" "-w" ];

  meta = {
    description = "Lattice Git task contract parser, types, and validator";
    homepage = "https://github.com/mytecor/lattice";
    license = lib.licenses.mit;
    mainProgram = "task-validate";
    platforms = lib.platforms.unix;
  };
}
