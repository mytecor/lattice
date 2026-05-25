{
  description = "Lattice wipe-root module";

  outputs = { ... }: {
    nixosModule = {
      imports = [
        ./options.nix
        ./config.nix
      ];
    };
  };
}
