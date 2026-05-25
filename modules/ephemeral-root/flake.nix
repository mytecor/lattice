{
  description = "Lattice ephemeral root module";

  inputs.impermanence.url = "github:nix-community/impermanence";

  outputs = { impermanence, ... }: {
    nixosModule = {
      imports = [
        impermanence.nixosModules.impermanence
        ./options.nix
        ./config.nix
      ];
    };
  };
}
