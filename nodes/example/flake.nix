{
  description = "Lattice node example";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    disko.url = "github:nix-community/disko";
    nixos-hardware.url = "github:NixOS/nixos-hardware";
    impermanence.url = "github:nix-community/impermanence";

    hardware = {
      url = "path:../../hardware/intel-n100";
      flake = false;
    };

    ephemeral-root = {
      url = "path:../../modules/ephemeral-root";
      flake = false;
    };

    wireless = {
      url = "path:../../modules/wireless";
      flake = false;
    };
  };

  outputs = { nixpkgs, disko, nixos-hardware, impermanence, hardware, ephemeral-root, wireless, ... }: {
    nixosConfigurations.example = nixpkgs.lib.nixosSystem {
      specialArgs = { inherit nixos-hardware; };

      modules = [
        # Hardware layer: platform, drivers, disks, filesystems, boot.
        hardware
        disko.nixosModules.disko
        ./disko.nix
        impermanence.nixosModules.impermanence
        ephemeral-root
        wireless

        # Modules layer: direct service modules, when a profile is not enough.
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
