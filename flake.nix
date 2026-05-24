{
  description = "Lattice node deployment flake";

  inputs = {
    example.url = "./nodes/example";
    # example.url = "git+https://github.com/mytecor/lattice.git?dir=nodes/example";
  };

  outputs = inputs: {
    nixosConfigurations = {
      example = inputs.example.nixosConfigurations.example;
    };
  };
}
