{
  description = "Lattice Intel N100 hardware module";

  inputs = {
    nixos-hardware.url = "github:NixOS/nixos-hardware";
  };

  outputs = { nixos-hardware, ... }: {
    nixosModule = import ./module.nix { inherit nixos-hardware; };
  };
}
