{
  description = "Lattice rns-server module";

  inputs.rns-rs.url = "../../packages/rns-rs";

  outputs = { rns-rs, ... }: {
    nixosModule = { pkgs, ... }: {
      imports = [
        ./options.nix
        ./config.nix
      ];

      _module.args.rnsServerPackage = rns-rs.packages.${pkgs.stdenv.hostPlatform.system}.rns-server;
    };
  };
}
