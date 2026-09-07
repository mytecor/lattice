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

    module-llm-gateway = {
      url = "path:./modules/llm-gateway";
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
    module-llm-gateway,
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
          comin-source-sync = final.writeShellApplication {
            name = "lattice-comin-source-sync";
            runtimeInputs = [ final.coreutils final.git final.jq final.util-linux ];
            text = builtins.readFile ./profiles/gitops/comin-source-sync.sh;
          };
          rns-server = final.callPackage "${rns-rs}/package.nix" { bin = "rns-server"; };
          rnsh = final.callPackage "${rns-rs}/package.nix" { bin = "rnsh"; };
          llm-gateway = final.callPackage ./packages/llm-gateway/package.nix { };
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
          inherit (pkgs.lattice) llm-gateway rns-server rnsh;
          default = pkgs.lattice.rns-server;
        });

      checks.x86_64-linux =
        let
          pkgs = import nixpkgs {
            system = "x86_64-linux";
            overlays = [ overlay ];
          };
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
          rns-network = import ./tests/rns-network.nix {
            inherit nixpkgs pkgs;
            rnsModule = self.nixosModules.rns-server;
            networkProfile = "${profiles}/rns-network/config.nix";
          };

          rns-tcp = import ./tests/rns-tcp.nix {
            inherit nixpkgs pkgs;
            rnsModule = self.nixosModules.rns-server;
            rnsProfile = "${profiles}/rns-server/config.nix";
          };

          app-services = import ./tests/app-services.nix {
            inherit nixpkgs pkgs;
            appServicesProfile = "${profiles}/app-services/config.nix";
          };

          llm-gateway-bifrost = import ./tests/llm-gateway-bifrost.nix {
            inherit nixpkgs pkgs;
            gatewayModule = self.nixosModules.llm-gateway;
            gatewayProfile = "${profiles}/llm-gateway/config.nix";
          };

          llm-gateway-service = import ./tests/llm-gateway-service.nix {
            inherit pkgs;
            gatewayModule = self.nixosModules.llm-gateway;
            gatewayProfile = "${profiles}/llm-gateway/config.nix";
          };

          comin-source-sync = import ./tests/comin-source-sync.nix {
            inherit pkgs;
            syncPackage = pkgs.lattice.comin-source-sync;
          };

          example =
            assert exampleConfig.services.comin.enable;
            assert exampleConfig.nix.settings.auto-optimise-store;
            assert exampleConfig.nix.gc.automatic;
            exampleConfig.system.build.toplevel;

          ephemeral-root-module =
            assert exampleConfig.lattice.ephemeral-root.enable;
            assert builtins.hasAttr "lattice-ephemeral-root" exampleConfig.boot.initrd.systemd.services;
            assert exampleConfig.boot.initrd.systemd.services.lattice-ephemeral-root.serviceConfig.RemainAfterExit;
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
            assert map (remote: remote.name) homelabConfig.services.comin.remotes
              == [ "source" ];
            assert (builtins.elemAt homelabConfig.services.comin.remotes 0).url
              == "/var/lib/comin/source/repository";
            assert builtins.hasAttr "lattice-comin-source-sync" homelabConfig.systemd.services;
            assert builtins.hasAttr "lattice-comin-source-sync" homelabConfig.systemd.timers;
            assert homelabConfig.services.openssh.enable;
            assert !homelabConfig.services.openssh.settings.PasswordAuthentication;
            assert homelabConfig.services.openssh.settings.PermitRootLogin == "prohibit-password";
            assert builtins.elem 22 homelabConfig.networking.firewall.allowedTCPPorts;
            assert homelabConfig.services.avahi.enable;
            assert homelabConfig.age.identityPaths == [ "/persist/var/lib/lattice/age/identity" ];
            assert builtins.hasAttr "wifi-ssid" homelabConfig.age.secrets;
            assert builtins.hasAttr "wifi-password" homelabConfig.age.secrets;
            assert builtins.hasAttr "radicle-private-key" homelabConfig.age.secrets;
            assert !homelabConfig.users.mutableUsers;
            assert builtins.length homelabConfig.lattice.wireless.networks == 1;
            assert homelabConfig.lattice.rns-server.enable;
            assert homelabConfig.lattice.rns-server.reticulum.enable_transport;
            assert !homelabConfig.lattice.rns-server.server.http.enabled;
            assert homelabConfig.lattice.rnsh.enable;
            assert !homelabConfig.lattice.rnsh.noAuth;
            assert homelabConfig.lattice.rnsh.allowed != [ ];
            assert homelabConfig.lattice.rnsh.user == "rnsh";
            assert homelabConfig.services.radicle.enable;
            assert homelabConfig.services.radicle.node.listenPort == 8776;
            assert !homelabConfig.services.radicle.node.openFirewall;
            assert homelabConfig.services.radicle.httpd.enable;
            assert homelabConfig.services.radicle.httpd.listenAddress == "127.0.0.1";
            assert homelabConfig.services.radicle.httpd.aliases.lattice
              == "rad:z3AqC22BKQ5Gnrkw49N7PGJa91G6L";
            assert homelabConfig.services.radicle.settings.node.alias == "mytecor-homelab";
            assert homelabConfig.services.radicle.settings.node.seedingPolicy.default == "block";
            assert homelabConfig.services.radicle.settings.web.pinned.repositories
              == [ "rad:z3AqC22BKQ5Gnrkw49N7PGJa91G6L" ];
            assert builtins.hasAttr "radicle-seed-lattice" homelabConfig.systemd.services;
            assert homelabConfig.systemd.services.radicle-seed-lattice.serviceConfig.Restart
              == "on-failure";
            assert nixpkgs.lib.hasInfix "rad-system seed --scope followed"
              homelabConfig.systemd.services.radicle-seed-lattice.serviceConfig.ExecStart;
            assert builtins.elem
              "dev.radicle.node.secret:/run/agenix/radicle-private-key"
              homelabConfig.systemd.services.radicle-node.serviceConfig.LoadCredential;
            assert builtins.any
              (entry:
                if builtins.isString entry then
                  entry == "/var/lib/radicle"
                else
                  entry.directory == "/var/lib/radicle")
              homelabConfig.environment.persistence."/persist".directories;
            assert !homelabConfig.services.nginx.enable;
            assert homelabConfig.services.caddy.enable;
            assert builtins.hasAttr
              "http://status.mytecor-homelab.local"
              homelabConfig.services.caddy.virtualHosts;
            assert builtins.hasAttr
              "http://radicle.mytecor-homelab.local"
              homelabConfig.services.caddy.virtualHosts;
            assert builtins.hasAttr
              "http://llm-gateway.mytecor-homelab.local"
              homelabConfig.services.caddy.virtualHosts;
            assert nixpkgs.lib.hasInfix "lattice-node-status"
              homelabConfig.services.caddy.virtualHosts
                ."http://status.mytecor-homelab.local".extraConfig;
            assert builtins.hasAttr "node-status-mdns" homelabConfig.systemd.services;
            assert nixpkgs.lib.hasInfix "status.mytecor-homelab.local"
              homelabConfig.systemd.services.node-status-mdns.script;
            assert builtins.hasAttr "radicle-mdns" homelabConfig.systemd.services;
            assert nixpkgs.lib.hasInfix "radicle.mytecor-homelab.local"
              homelabConfig.systemd.services.radicle-mdns.script;
            assert nixpkgs.lib.hasInfix "reverse_proxy 127.0.0.1:9208"
              homelabConfig.services.caddy.virtualHosts
                ."http://llm-gateway.mytecor-homelab.local".extraConfig;
            assert homelabConfig.lattice.llm-gateway.package == pkgs.lattice.llm-gateway;
            assert builtins.length (builtins.attrNames homelabConfig.lattice.llm-gateway.providers) == 2;
            assert homelabConfig.lattice.llm-gateway.providers.proxy.modelsUrl == null;
            assert homelabConfig.lattice.llm-gateway.providers.openbroker.modelsUrl
              == "https://proxy.gonka.gg/v1/models";
            assert nixpkgs.lib.all
              (rule: rule.action != "retry" || rule.attempts == 10)
              homelabConfig.lattice.llm-gateway.routingRules;
            assert nixpkgs.lib.hasInfix "lattice-llm-gateway"
              homelabConfig.systemd.services.llm-gateway.serviceConfig.ExecStart;
            assert builtins.hasAttr "llm-gateway-mdns" homelabConfig.systemd.services;
            assert nixpkgs.lib.hasInfix "llm-gateway.mytecor-homelab.local"
              homelabConfig.systemd.services.llm-gateway-mdns.script;
            assert homelabConfig.networking.firewall.allowedTCPPorts == [ 22 80 ];
            assert nixpkgs.lib.hasInfix "--config /var/lib/rnsh --rnsconfig /var/lib/rns"
              homelabConfig.systemd.services.rnsh.serviceConfig.ExecStart;
            homelabConfig.system.build.toplevel;
        };

      nixosModules = {
        ephemeral-root.imports = [ "${module-ephemeral-root}" ];
        rns-server.imports = [ "${module-rns-server}" ];
        rnsh.imports = [ "${module-rnsh}" ];
        llm-gateway.imports = [ "${module-llm-gateway}" ];
        wireless.imports = [ "${module-wireless}" ];

        default.imports = [
          self.nixosModules.ephemeral-root
          self.nixosModules.rns-server
          self.nixosModules.rnsh
          self.nixosModules.llm-gateway
          self.nixosModules.wireless
        ];
      };

      nixosConfigurations.example = mkNode {
        imports = [ ./nodes/example "${profiles}/rns-server/config.nix" ];
      };
      nixosConfigurations.mytecor-homelab = mkNode {
        imports = [
          ./nodes/mytecor-homelab
          "${profiles}/app-services/config.nix"
          "${profiles}/llm-gateway/config.nix"
          "${profiles}/radicle/config.nix"
          "${profiles}/rns-network/config.nix"
          "${profiles}/rnsh/config.nix"
        ];
      };
    };
}
