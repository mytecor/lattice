{
  description = "Lattice wireless module";

  outputs = { ... }: {
    nixosModule = {
      imports = [
        ./options.nix
        ./config.nix
        ./env.nix
      ];
    };
  };
}
