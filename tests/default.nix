{ self, nixpkgs, impermanence, profiles, overlay }:

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
  rns-network = import ./rns-network.nix {
    inherit nixpkgs pkgs;
    rnsModule = self.nixosModules.rns-server;
    networkProfile = "${profiles}/rns-network/config.nix";
  };

  rns-tcp = import ./rns-tcp.nix {
    inherit nixpkgs pkgs;
    rnsModule = self.nixosModules.rns-server;
    rnsProfile = "${profiles}/rns-server/config.nix";
  };

  app-services = import ./app-services.nix {
    inherit nixpkgs pkgs;
    appServicesProfile = "${profiles}/app-services/config.nix";
  };

  llm-gateway-bifrost = import ./llm-gateway-bifrost.nix {
    inherit nixpkgs pkgs;
    gatewayModule = self.nixosModules.llm-gateway;
    gatewayProfile = "${profiles}/llm-gateway/config.nix";
  };

  llm-gateway-service = import ./llm-gateway-service.nix {
    inherit pkgs nixpkgs;
    gatewayModule = self.nixosModules.llm-gateway;
    gatewayProfile = "${profiles}/llm-gateway/config.nix";
  };

  pi-config = import ./pi-config.nix {
    inherit pkgs;
    piModule = self.nixosModules.pi;
  };

  pi-tool-profile = import ./pi-tool-profile.nix {
    inherit nixpkgs pkgs;
    piModule = self.nixosModules.pi;
  };

  pi-acp-daemon = import ./pi-acp-daemon.nix {
    inherit nixpkgs pkgs;
    acpModule = self.nixosModules.pi-acp-daemon;
    gatewayProfile = "${profiles}/tcp-gateway/config.nix";
  };

  wireless-hotspot = import ./wireless-hotspot.nix {
    inherit nixpkgs pkgs;
    hotspotModule = self.nixosModules.hotspot;
  };

  comin-source-sync = import ./comin-source-sync.nix {
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

  # High-level safety / architecture / security invariants for the production
  # node. NOT a snapshot of the config: changing an operational value (provider
  # URLs, model lists, routing-rule counts, ports, exact settings) is a
  # legitimate configuration change and must not fail this test. Only
  # constraints that are morally required for this node hold here; everything
  # else is covered by module assertions (rns-network, rns-tcp, llm-gateway)
  # and by the generated-artifact / runtime tests. If a rule turns out to be a
  # rule of the module itself, it belongs in the module's `assertions`, not in
  # this node-specific test.
  mytecor-homelab =
    assert homelabConfig.lattice.ephemeral-root.enable;
    assert homelabConfig.lattice.rns-server.enable;
    assert homelabConfig.lattice.llm-gateway.enable;
    assert homelabConfig.lattice.pi.enable;
    assert homelabConfig.lattice.rnsh.enable;
    assert homelabConfig.services.comin.enable;
    # SSH must stay key-only on the public-facing node.
    assert homelabConfig.services.openssh.enable;
    assert !homelabConfig.services.openssh.settings.PasswordAuthentication;
    assert homelabConfig.services.openssh.settings.PermitRootLogin == "prohibit-password";
    assert !homelabConfig.users.mutableUsers;
    # Reverse proxy is Caddy-only (no nginx) serving the local service mesh.
    assert homelabConfig.services.caddy.enable;
    assert !homelabConfig.services.nginx.enable;
    # SSH remains reachable through the firewall.
    assert builtins.elem 22 homelabConfig.networking.firewall.allowedTCPPorts;
    homelabConfig.system.build.toplevel;
}

