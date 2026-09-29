{ lib, buildGo127Module }:

buildGo127Module {
  pname = "lattice-node-status";
  version = "0.2.0";

  src = ./.;
  vendorHash = null;
  doCheck = true;

  ldflags = [ "-s" "-w" "-X main.version=0.2.0" ];

  meta = {
    description = "Lattice node status API and Prometheus system metrics exporter";
    homepage = "https://github.com/mytecor/lattice";
    license = lib.licenses.mit;
    mainProgram = "node-status";
    platforms = lib.platforms.linux;
  };
}
