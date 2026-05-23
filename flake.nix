{
  description = "Lattice node deployment flake";

  inputs = {
    # node-name.url = "./nodes/node-name";
  };

  outputs = inputs: {
    nixosConfigurations = {
      # node-name = inputs.node-name.nixosConfigurations.node-name;
    };
  };
}
