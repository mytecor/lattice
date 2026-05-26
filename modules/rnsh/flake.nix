{
  description = "Lattice rnsh module";

  inputs.rns-rs.url = "../../packages/rns-rs";

  outputs = { rns-rs, ... }: {
    nixosModule = { pkgs, ... }: {
      imports = [
        ./options.nix
        ./config.nix
      ];

      _module.args.rnshPackage = rns-rs.packages.${pkgs.stdenv.hostPlatform.system}.rnsh;
    };
  };
}
