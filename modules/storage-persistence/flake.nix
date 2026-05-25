{
  description = "Lattice persistence module";

  inputs.impermanence.url = "github:nix-community/impermanence";

  outputs = { impermanence, ... }: {
    nixosModule = {
      imports = [
        impermanence.nixosModules.impermanence
        ./config.nix
      ];
    };
  };
}
