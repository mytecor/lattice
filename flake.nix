{
  description = "Lattice node deployment flake";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

    disko = {
      url = "github:nix-community/disko";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    impermanence = {
      url = "github:nix-community/impermanence";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    comin = {
      url = "github:nlewo/comin";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    nixos-hardware = {
      url = "github:NixOS/nixos-hardware";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    hardware-intel-n100 = {
      url = "path:./hardware/intel-n100";
      flake = false;
    };

    module-ephemeral-root = {
      url = "path:./modules/ephemeral-root";
      flake = false;
    };

    module-rns-server = {
      url = "path:./modules/rns-server";
      flake = false;
    };

    module-rnsh = {
      url = "path:./modules/rnsh";
      flake = false;
    };

    module-wireless = {
      url = "path:./modules/wireless";
      flake = false;
    };

    profiles = {
      url = "path:./profiles";
      flake = false;
    };

    rns-rs = {
      url = "path:./packages/rns-rs";
      flake = false;
    };
  };

  outputs = {
    self,
    nixpkgs,
    comin,
    disko,
    impermanence,
    nixos-hardware,
    hardware-intel-n100,
    module-ephemeral-root,
    module-rns-server,
    module-rnsh,
    module-wireless,
    profiles,
    rns-rs,
    ...
  }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;

      overlay = final: _previous: {
        lattice = {
          rns-server = final.callPackage "${rns-rs}/package.nix" { bin = "rns-server"; };
          rnsh = final.callPackage "${rns-rs}/package.nix" { bin = "rnsh"; };
        };
      };
    in
    {
      overlays.default = overlay;

      packages = forAllSystems (system:
        let
          pkgs = import nixpkgs {
            inherit system;
            overlays = [ overlay ];
            config.allowUnfreePredicate = package:
              builtins.elem (nixpkgs.lib.getName package) [ "rns-server" "rnsh" ];
          };
        in
        {
          inherit (pkgs.lattice) rns-server rnsh;
          default = pkgs.lattice.rns-server;
        });

      checks.x86_64-linux.example =
        let
          config = self.nixosConfigurations.example.config;
        in
        assert config.services.comin.enable;
        assert config.nix.settings.auto-optimise-store;
        assert config.nix.gc.automatic;
        config.system.build.toplevel;

      nixosModules = {
        ephemeral-root.imports = [ "${module-ephemeral-root}" ];
        rns-server.imports = [ "${module-rns-server}" ];
        rnsh.imports = [ "${module-rnsh}" ];
        wireless.imports = [ "${module-wireless}" ];

        default.imports = [
          self.nixosModules.ephemeral-root
          self.nixosModules.rns-server
          self.nixosModules.rnsh
          self.nixosModules.wireless
        ];
      };

      nixosConfigurations.example = nixpkgs.lib.nixosSystem {
        specialArgs = { inherit nixos-hardware; };

        modules = [
          { nixpkgs.overlays = [ overlay ]; }

          "${hardware-intel-n100}"
          disko.nixosModules.disko
          impermanence.nixosModules.impermanence
          comin.nixosModules.comin
          self.nixosModules.default

          "${profiles}/base"
          ./nodes/example
        ];
      };
    };
}
