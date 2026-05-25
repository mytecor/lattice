{
  description = "Lattice node example";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    disko.url = "github:nix-community/disko";

    hardware.url = "path:../../hardware/intel-n100";

    example-package = {
      url = "path:../../packages/example";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    persistence = {
      url = "path:../../modules/storage-persistence";
    };

    wipe-root = {
      url = "path:../../modules/wipe-root";
    };
  };

  outputs = inputs@{ nixpkgs, disko, hardware, example-package, persistence, wipe-root, ... }: {
    nixosConfigurations.example = nixpkgs.lib.nixosSystem {
      specialArgs = { inherit inputs example-package; };

      modules = [
        # Hardware layer: platform, drivers, disks, filesystems, boot.
        hardware.nixosModule
        disko.nixosModules.disko
        ./disko.nix
        wipe-root.nixosModule

        # Modules layer: direct service modules, when a profile is not enough.
        persistence.nixosModule
        # inputs.modules.nixosModules.service-name

        # Profiles layer: reusable node roles.
        # profiles.nixosModules.base
        # profiles.nixosModules.reticulum-node

        # Node layer: hostname, secrets, files and local overrides.
        ./config.nix
      ];
    };
  };
}
