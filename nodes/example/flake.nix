{
  description = "Lattice node example";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

    hardware.url = "path:../../hardware/intel-n100";
    profiles.url = "path:../../profiles";
    modules.url = "path:../../modules";

    example-package = {
      url = "path:../../packages/example";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    disko = {
      url = "github:nix-community/disko";
      inputs.nixpkgs.follows = "nixpkgs";
    };

  };

  outputs = inputs@{ nixpkgs, disko, hardware, profiles, example-package, ... }: {
    nixosConfigurations.example = nixpkgs.lib.nixosSystem {
      specialArgs = { inherit inputs example-package; };

      modules = [
        # External modules
        disko.nixosModules.disko

        # Hardware layer: platform, drivers, disks, filesystems, boot.
        hardware.nixosModule

        # Profiles layer: reusable node roles.
        # profiles.nixosModules.base
        # profiles.nixosModules.reticulum-node

        # Modules layer: direct service modules, when a profile is not enough.
        # inputs.modules.nixosModules.service-name

        # Node layer: hostname, secrets, files and local overrides.
        ./config.nix
      ];
    };
  };
}
