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

    module-pi = {
      url = "path:./modules/pi";
      flake = false;
    };

    module-pi-acp-daemon = {
      url = "path:./modules/pi-acp-daemon";
      flake = false;
    };

    module-wireless = {
      url = "path:./modules/wireless";
      flake = false;
    };

    module-git-cache-proxy = {
      url = "path:./modules/git-cache-proxy";
      flake = false;
    };

    module-verdaccio = {
      url = "path:./modules/verdaccio";
      flake = false;
    };

    module-observability-prometheus = {
      url = "path:./modules/observability-prometheus";
      flake = false;
    };

    module-observability-loki = {
      url = "path:./modules/observability-loki";
      flake = false;
    };

    module-observability-alloy = {
      url = "path:./modules/observability-alloy";
      flake = false;
    };

    module-grafana = {
      url = "path:./modules/grafana";
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
    module-pi,
    module-pi-acp-daemon,
    module-wireless,
    module-git-cache-proxy,
    module-verdaccio,
    module-observability-prometheus,
    module-observability-loki,
    module-observability-alloy,
    module-grafana,
    profiles,
    rns-rs,
    ...
  }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;

      overlay = final: _previous: {
        buildPnpmCli = final.callPackage ./packages/pnpm-cli-builder/package.nix { };

        lattice = {
          comin-source-sync = final.writeShellApplication {
            name = "lattice-comin-source-sync";
            runtimeInputs = [ final.coreutils final.git final.jq final.util-linux ];
            text = builtins.readFile ./profiles/gitops/comin-source-sync.sh;
          };
          # f4-04: writer runtime-статуса узла (generation/commit JSON). Используется
          # активационным скриптом app-services и контрактным тестом node-status.
          node-status-write = final.writeShellApplication {
            name = "lattice-node-status-write";
            runtimeInputs = [ final.coreutils final.git final.jq ];
            text = builtins.readFile ./profiles/app-services/status-write.sh;
          };
          acp-normalizer = final.callPackage ./packages/acp-normalizer/package.nix { };
          rns-server = final.callPackage "${rns-rs}/package.nix" { bin = "rns-server"; };
          rnsh = final.callPackage "${rns-rs}/package.nix" { bin = "rnsh"; };
          hydra-acp = final.callPackage ./packages/hydra-acp/package.nix { };
          llm-gateway = final.callPackage ./packages/llm-gateway/package.nix { };
          pi = final.callPackage ./packages/pi/package.nix { };
          pi-mcp-adapter = final.callPackage ./packages/pi-mcp-adapter/package.nix { };
          pi-acp = final.callPackage ./packages/pi-acp/package.nix {
            pi = final.lattice.pi;
          };
          git-cache-proxy = final.callPackage ./packages/git-cache-proxy/package.nix { };
          verdaccio = final.callPackage ./packages/verdaccio/package.nix { };

          # f8-03: воспроизводимый tool profile для Pi-рантайма.
          pi-tool-profile = final.buildEnv {
            name = "lattice-pi-tool-profile";
            paths = (import ./profiles/pi/base-tools.nix { pkgs = final; }).base;
          };

          # devShell контракт: тот же базовый набор bash/git/tools, что и на ноде.
          pi-develop-shell = final.mkShell {
            packages = (import ./profiles/pi/base-tools.nix { pkgs = final; }).base;
            shellHook = ''
              # f8-03: фиксируем контракт окружения в интерактивной оболочке.
              export LANG=C.UTF-8
              export LC_ALL=C.UTF-8
            '';
          };
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
          inherit (pkgs.lattice) acp-normalizer git-cache-proxy hydra-acp llm-gateway pi pi-acp pi-mcp-adapter pi-tool-profile rns-server rnsh verdaccio;
          default = pkgs.lattice.rns-server;
        });

      devShells = forAllSystems (system:
        let
          pkgs = import nixpkgs {
            inherit system;
            overlays = [ overlay ];
            config.allowUnfreePredicate = package:
              builtins.elem (nixpkgs.lib.getName package) [ "rns-server" "rnsh" ];
          };
        in
        {
          default = pkgs.lattice.pi-develop-shell;
        });

      checks.x86_64-linux = import ./tests {
        inherit self nixpkgs impermanence profiles overlay;
      };

      nixosModules = {
        ephemeral-root.imports = [ "${module-ephemeral-root}" ];
        rns-server.imports = [ "${module-rns-server}" ];
        rnsh.imports = [ "${module-rnsh}" ];
        llm-gateway.imports = [ "${module-llm-gateway}" ];
        pi.imports = [ "${module-pi}" ];
        pi-acp-daemon.imports = [ "${module-pi-acp-daemon}" ];
        wireless.imports = [ "${module-wireless}" ];
        git-cache-proxy.imports = [ "${module-git-cache-proxy}" ];
        verdaccio.imports = [ "${module-verdaccio}" ];
        observability-prometheus.imports = [ "${module-observability-prometheus}" ];
        observability-loki.imports = [ "${module-observability-loki}" ];
        observability-alloy.imports = [ "${module-observability-alloy}" ];
        grafana.imports = [ "${module-grafana}" ];

        default.imports = [
          self.nixosModules.ephemeral-root
          self.nixosModules.rns-server
          self.nixosModules.rnsh
          self.nixosModules.llm-gateway
          self.nixosModules.pi
          self.nixosModules.pi-acp-daemon
          self.nixosModules.wireless
          self.nixosModules.git-cache-proxy
          self.nixosModules.verdaccio
          self.nixosModules.observability-prometheus
          self.nixosModules.observability-loki
          self.nixosModules.observability-alloy
          self.nixosModules.grafana
        ];
      };

      nixosConfigurations.example = mkNode {
        imports = [ ./nodes/example "${profiles}/rns-server/config.nix" ];
      };
      nixosConfigurations.mytecor-homelab = mkNode {
        imports = [
          ./nodes/mytecor-homelab
          "${profiles}/app-services/config.nix"
          "${profiles}/cache-plane/config.nix"
          "${profiles}/observability/config.nix"
          "${profiles}/llm-gateway/config.nix"
          "${profiles}/pi-acp/config.nix"
          "${profiles}/radicle/config.nix"
          "${profiles}/rns-network/config.nix"
          "${profiles}/rnsh/config.nix"
        ];
      };
    };
}
