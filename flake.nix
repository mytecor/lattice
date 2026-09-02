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

    agenix = {
      url = "github:ryantm/agenix";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.darwin.follows = "";
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
    agenix,
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

      mkNode = nodeModule: nixpkgs.lib.nixosSystem {
        specialArgs = { inherit nixos-hardware; };

        modules = [
          { nixpkgs.overlays = [ overlay ]; }

          "${hardware-intel-n100}"
          disko.nixosModules.disko
          impermanence.nixosModules.impermanence
          agenix.nixosModules.default
          comin.nixosModules.comin
          self.nixosModules.default

          "${profiles}/base"
          nodeModule
        ];
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

      checks.x86_64-linux =
        let
          pkgs = import nixpkgs { system = "x86_64-linux"; };
          exampleConfig = self.nixosConfigurations.example.config;
          homelabConfig = self.nixosConfigurations.mytecor-homelab.config;
          disabledConfig = (nixpkgs.lib.nixosSystem {
            modules = [
              { nixpkgs.hostPlatform = "x86_64-linux"; system.stateVersion = "26.05"; }
              impermanence.nixosModules.impermanence
              self.nixosModules.ephemeral-root
            ];
          }).config;
        in
        {
          example =
            assert exampleConfig.services.comin.enable;
            assert exampleConfig.nix.settings.auto-optimise-store;
            assert exampleConfig.nix.gc.automatic;
            exampleConfig.system.build.toplevel;

          ephemeral-root-module =
            assert exampleConfig.lattice.ephemeral-root.enable;
            assert builtins.hasAttr "lattice-ephemeral-root" exampleConfig.boot.initrd.systemd.services;
            assert builtins.any
              (entry: nixpkgs.lib.hasPrefix
                (toString entry.source)
                exampleConfig.boot.initrd.systemd.services.lattice-ephemeral-root.serviceConfig.ExecStart)
              exampleConfig.boot.initrd.systemd.storePaths;
            assert builtins.hasAttr "lattice-ephemeral-root-prune" exampleConfig.systemd.services;
            assert exampleConfig.fileSystems."/persist".neededForBoot;
            assert !disabledConfig.lattice.ephemeral-root.enable;
            assert !(builtins.hasAttr "lattice-ephemeral-root" disabledConfig.boot.initrd.systemd.services);
            assert !(builtins.hasAttr "/data" exampleConfig.environment.persistence);
            pkgs.runCommand "ephemeral-root-module-evaluation" { } "touch $out";

          mytecor-homelab =
            assert homelabConfig.networking.hostName == "mytecor-homelab";
            assert homelabConfig.lattice.ephemeral-root.enable;
            assert homelabConfig.services.comin.enable;
            assert homelabConfig.services.openssh.enable;
            assert !homelabConfig.services.openssh.settings.PasswordAuthentication;
            assert homelabConfig.services.openssh.settings.PermitRootLogin == "prohibit-password";
            assert builtins.elem 22 homelabConfig.networking.firewall.allowedTCPPorts;
            assert homelabConfig.services.avahi.enable;
            assert homelabConfig.age.identityPaths == [ "/persist/var/lib/lattice/age/identity" ];
            assert builtins.length (builtins.attrNames homelabConfig.age.secrets) == 2;
            assert builtins.length homelabConfig.lattice.wireless.networks == 1;
            homelabConfig.system.build.toplevel;
        };

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

      nixosConfigurations.example = mkNode ./nodes/example;
      nixosConfigurations.mytecor-homelab = mkNode ./nodes/mytecor-homelab;
    };
}
