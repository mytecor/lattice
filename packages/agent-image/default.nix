{ pkgs ? import <nixpkgs> { } }:

pkgs.callPackage ./package.nix {
  pi = pkgs.lattice.pi or pkgs.callPackage ../pi/package.nix { };
  pi-acp = pkgs.lattice.pi-acp or pkgs.callPackage ../pi-acp/package.nix { };
  pi-tool-profile = pkgs.lattice.pi-tool-profile or (pkgs.buildEnv {
    name = "lattice-pi-tool-profile";
    paths = (import ../../profiles/pi/base-tools.nix { inherit pkgs; }).base;
  });
  hydra-acp = pkgs.lattice.hydra-acp or pkgs.callPackage ../hydra-acp/package.nix { };
}
