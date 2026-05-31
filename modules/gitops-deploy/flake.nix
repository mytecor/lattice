{
  description = "Lattice GitOps deploy module";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    upstream-comin = {
      url = "github:nlewo/comin";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { upstream-comin, ... }: {
    nixosModule = { ... }: {
      imports = [
        upstream-comin.nixosModules.comin
        ./config.nix
      ];
    };
  };
}
