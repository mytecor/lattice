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
          inherit (pkgs.lattice) acp-normalizer hydra-acp llm-gateway pi pi-acp pi-mcp-adapter pi-tool-profile rns-server rnsh;
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

        default.imports = [
          self.nixosModules.ephemeral-root
          self.nixosModules.rns-server
          self.nixosModules.rnsh
          self.nixosModules.llm-gateway
          self.nixosModules.pi
          self.nixosModules.pi-acp-daemon
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
          "${profiles}/pi-acp/config.nix"
          "${profiles}/radicle/config.nix"
          "${profiles}/rns-network/config.nix"
          "${profiles}/rnsh/config.nix"
        ];
      };
    };
}
