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

  # The cache-plane profile (cache-plane/config.nix) composes the cache
  # services, so any isolated test that imports it must provide all the modules
  # (git-cache-proxy, verdaccio). Defined here (in the `let`, not in the result
  # attribute set) so the test entries below can reference it.
  cachePlaneModules = [
    self.nixosModules.git-cache-proxy
    self.nixosModules.verdaccio
  ];

  # F12 observability stack modules, composed so the isolated contract test can
  # enable them together with the observability profile.
  observabilityModules = [
    self.nixosModules.observability-prometheus
    self.nixosModules.observability-loki
    self.nixosModules.observability-alloy
    self.nixosModules.grafana
  ];
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

  pi-tool-profile = import ./pi-tool-profile.nix {
    inherit nixpkgs pkgs;
    piModule = self.nixosModules.pi;
  };

  pi-models-config = import ./pi-models-config.nix {
    inherit nixpkgs pkgs;
    piModule = self.nixosModules.pi;
  };

  pi-acp-daemon = import ./pi-acp-daemon.nix {
    inherit nixpkgs pkgs;
    acpModule = self.nixosModules.pi-acp-daemon;
    gatewayProfile = "${profiles}/tcp-gateway/config.nix";
  };

  git-cache-proxy-config = import ./git-cache-proxy-config.nix {
    inherit pkgs nixpkgs;
    cachePlaneModules = cachePlaneModules;
    gitCacheModule = self.nixosModules.git-cache-proxy;
    gitCacheProfile = "${profiles}/cache-plane/config.nix";
    gatewayProfile = "${profiles}/tcp-gateway/config.nix";
  };

  verdaccio = import ./verdaccio.nix {
    inherit pkgs nixpkgs;
    cachePlaneModules = cachePlaneModules;
    verdaccioModule = self.nixosModules.verdaccio;
    verdaccioProfile = "${profiles}/cache-plane/config.nix";
  };

  observability-stack = import ./observability-stack.nix {
    inherit nixpkgs pkgs;
    lib = nixpkgs.lib;
    observabilityModules = observabilityModules;
    observabilityProfile = "${profiles}/observability/config.nix";
  };

  grafana-dashboards = import ./grafana-dashboards.nix {
    inherit nixpkgs pkgs;
    lib = nixpkgs.lib;
    observabilityModules = observabilityModules;
    observabilityProfile = "${profiles}/observability/config.nix";
  };

  grafana-ingress = import ./grafana-ingress.nix {
    inherit nixpkgs pkgs;
    observabilityModules = observabilityModules;
    observabilityProfile = "${profiles}/observability/config.nix";
    gatewayProfile = "${profiles}/tcp-gateway/config.nix";
  };

  comin-source-sync = import ./comin-source-sync.nix {
    inherit pkgs;
    syncPackage = pkgs.lattice.comin-source-sync;
  };

  node-status = import ./node-status.nix {
    inherit pkgs;
    statusWriter = pkgs.lattice.node-status-write;
  };

  # f1-01: nodes/example must actually carry the base profile into the build.
  # profiles/base (imported for every node via mkNode) pulls in profiles/gitops
  # (services.comin), nix.settings.auto-optimise-store and nix.gc.automatic.
  # These asserts are the explicit regression guard: if base ever stops being
  # wired into the assembled system, this check fails even though evaluation
  # would still succeed. Live confirmation 2026-09-16 on mytecor-homelab:
  # comin.service / lattice-comin-source-sync.{service,timer} present, runtime
  # `nix show-config --auto-optimise-store` = true, nix-gc.timer scheduled.
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
    assert homelabConfig.lattice.git-cache-proxy.enable;
    # Cache-plane ingress stays loopback-only; the LAN exposure is Caddy's job.
    assert homelabConfig.lattice.git-cache-proxy.host == "127.0.0.1";
    # The proxy is a shared credentialed reader: no upstream credential may
    # exist until per-repo authorization (f9-02) is in place.
    assert homelabConfig.lattice.git-cache-proxy.upstreamAuthHeaderFile == null;
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

