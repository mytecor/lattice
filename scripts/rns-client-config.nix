# nix eval --impure --raw --file scripts/rns-client-config.nix
let
  flake = builtins.getFlake ("git+file://" + toString ../.);
in
(flake.inputs.nixpkgs.lib.nixosSystem {
  modules = [
    flake.nixosModules.rns-server
    ../profiles/rns-network/config.nix
    {
      nixpkgs.hostPlatform = "x86_64-linux";
      system.stateVersion = "26.05";
      lattice.rns-server = {
        # Only render configuration; no Linux binary is built or run on the client.
        package = flake.inputs.nixpkgs.legacyPackages.x86_64-linux.hello;
        reticulum = {
          instance_name = "lattice-client";
          shared_instance_port = 39428;
          instance_control_port = 39429;
        };
      };
    }
  ];
}).config.lattice.rns-server.configFile.text
