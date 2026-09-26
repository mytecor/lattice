{ self, nixpkgs, impermanence, profiles, overlay }:

let
  inherit (nixpkgs) lib;
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

  llm-gateway-sugar = import ./llm-gateway-sugar.nix {
    inherit nixpkgs pkgs;
    gatewayModule = self.nixosModules.llm-gateway;
  };

  pi-tool-profile = import ./pi-tool-profile.nix {
    inherit nixpkgs pkgs;
    piModule = self.nixosModules.pi;
  };

  pi-models-config = import ./pi-models-config.nix {
    inherit nixpkgs pkgs;
    piModule = self.nixosModules.pi;
  };

  pi-retry-config = import ./pi-retry-config.nix {
    inherit nixpkgs pkgs;
    piModule = self.nixosModules.pi;
  };

  # f18-11: contract test for the F18 browser agent stack (foxbridge-camoufox +
  # jev-ultrafast): lifecycle coupling, seccomp (⇺s@resources), persistent
  # browser home, loopback-only CDP, LoadCredential-only secrets. Catches the
  # f18-08 crash-loops (SIGSYS setpriority, tmpfs HOME) as eval-time regressions.
  f18-browser-stack = import ./f18-browser-stack.nix {
    inherit nixpkgs pkgs;
    foxbridgeModule = self.nixosModules.foxbridge-camoufox;
    jevModule = self.nixosModules.jev-ultrafast;
    browserProfile = "${profiles}/browser-agent-stack/config.nix";
  };

  pi-acp-daemon = import ./pi-acp-daemon.nix {
    inherit nixpkgs pkgs;
    acpModule = self.nixosModules.pi-acp-daemon;
    gatewayProfile = "${profiles}/tcp-gateway/config.nix";
  };

  worker-runtime = import ./worker-runtime.nix {
    inherit nixpkgs pkgs;
    workerRuntimeModule = self.nixosModules.worker-runtime;
  };

  # f4-05: тcp-gateway mesh-ингресс поверх LAN-контракта (meshDomain / cloudflare).
  tcp-gateway-mesh = import ./tcp-gateway-mesh.nix {
    inherit nixpkgs pkgs;
    llmGatewayModule = self.nixosModules.llm-gateway;
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
    inherit nixpkgs pkgs self;
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

  # f14-01: central SSO (Authentik) wiring: loopback binding, auth Caddy site
  # (LAN + mesh), ForwardAuth wrapping of the acp-ui browser UI, backend port
  # out of the firewall, non-root systemd units, secrets via EnvironmentFile.
  authentik = import ./authentik.nix {
    inherit nixpkgs pkgs;
    authentikModule = self.nixosModules.authentik;
    ssoProfile = "${profiles}/sso/config.nix";
    gatewayProfile = "${profiles}/tcp-gateway/config.nix";
    appServicesProfile = "${profiles}/app-services/config.nix";
    grafanaModule = self.nixosModules.grafana;
  };

  comin-source-sync = import ./comin-source-sync.nix {
    inherit pkgs;
    syncPackage = pkgs.lattice.comin-source-sync;
  };

  pnpm-deps-fod-reference = import ./pnpm-deps-fod-reference.nix {
    inherit pkgs lib;
    nix = pkgs.nix;
  };

  node-status = import ./node-status.nix {
    inherit pkgs;
    statusWriter = pkgs.lattice.node-status-write;
  };

  # acp-normalizer regression: the pure id-stability core (normalize.mjs) must
  # glue the chunks of one assistant reply under a single messageId AND start a
  # FRESH id for a new logical message even when the live transform stream only
  # delivers non-classic boundaries between turns (usage_update /
  # session_info_update — the kinds that reach the transformer between turns,
  # with no prompt_received / turn_complete). Catches the v0.1.0 cross-turn
  # messageId-collision that broke history rendering on session/load. Plain
  # node + assert, no build tool.
  acp-normalizer =
    let srcDir = "${self}/packages/acp-normalizer"; in
    pkgs.runCommand "acp-normalizer-test" {
      nativeBuildInputs = [ pkgs.nodejs ];
    } ''
      node ${srcDir}/normalize.test.mjs
      touch $out
    '';

  # acp-normalizer must PACKAGE the normalize.mjs core module next to its CLI
  # entry. The CLI does `import { createNormalize } from './normalize.mjs'` at
  # runtime, so a package that installs only acp-normalizer.mjs (the commit
  # 2385b51 split, before the installPhase was fixed) ships a broken
  # transformer that exits with ERR_MODULE_NOT_FOUND and silently does nothing
  # — exactly why the messageId fix never took effect on the node even after
  # the commit was deployed. The unit test above only runs normalize.test.mjs
  # straight from the source tree, which does NOT exercise the assembled
  # package, so it could not catch this. This test builds the real lattice
  # package and asserts the module ships and the CLI entry actually imports it.
  acp-normalizer-package =
    pkgs.runCommand "acp-normalizer-package-test" {
      nativeBuildInputs = [ pkgs.nodejs ];
    } ''
      bin=${pkgs.lattice.acp-normalizer}/bin/acp-normalizer
      test -x "$bin" || { echo "missing CLI $bin" >&2; exit 1; }
      test -f ${pkgs.lattice.acp-normalizer}/bin/normalize.mjs \
        || { echo "normalize.mjs not installed beside CLI" >&2; exit 1; }
      # The CLI parses ./normalize.mjs at import time (top-level import). A
      # missing module throws ERR_MODULE_NOT_FOUND immediately on load.
      NODE_PATH= node --input-type=module -e \
        "import('${pkgs.lattice.acp-normalizer}/bin/acp-normalizer')" 2>&1 \
        | grep -qi ERR_MODULE_NOT_FOUND \
        && { echo "CLI still cannot load normalize.mjs" >&2; exit 1; }
      touch $out
    '';

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
    # f4-05: Yggdrasil is the external transport; the node must run it with a
    # stable key from agenix (never a regenerated / ephemeral one).
    assert homelabConfig.services.yggdrasil.enable;
    # The private key is loaded from the agenix-decrypted file via systemd
    # credentials (PrivateKeyPath), never embedded in the Nix store as
    # settings.PrivateKey — the module asserts that globally.
    assert lib.hasPrefix
      "/run/agenix/yggdrasil-keys"
      homelabConfig.services.yggdrasil.settings.PrivateKeyPath;
    # At least one public peer must be configured or the mesh address is
    # unreachable from the internet.
    assert builtins.length homelabConfig.services.yggdrasil.settings.Peers > 0;
    # f4-05: the external mesh domain is what DNS AAAA records target; the node
    # opts into the parallel ingress. Changing the domain/peers is a legitimate
    # operational change, so only the fact that mesh ingress is active is pinned.
    assert homelabConfig.lattice.tcp-gateway.meshDomain != null;
    assert builtins.any
      (name: lib.hasSuffix ".homelab.myt.su" name)
      (builtins.attrNames homelabConfig.services.caddy.virtualHosts);
    # f4-05/F14 (policy): Grafana is now exposed on the public mesh but ONLY
    # because it is closed behind Authentik SSO (mesh site is a real Caddy
    # virtualHost, HTTPS because the Cloudflare token is present — so it is
    # https://grafana.homelab.myt.su, not http://). llm-gateway stays OFF the
    # public mesh (no TLS / API-key protection). This is the user's hard
    # constraint, not an operational value, so it is pinned here.
    let
      lanSuffix = ".mytecor-homelab.local";
      hosts = builtins.attrNames homelabConfig.services.caddy.virtualHosts;
      meshHosts = builtins.filter (n: lib.hasSuffix ".homelab.myt.su" n) hosts;
      graphLan = "http://grafana" + lanSuffix;
      graphMesh = "https://grafana.homelab.myt.su";
      llmLan = "http://llm-gateway" + lanSuffix;
      llmMesh = "http://llm-gateway.homelab.myt.su";
    in
    assert builtins.any (n: n == graphLan) hosts;
    assert builtins.any (n: n == graphMesh) meshHosts;
    assert builtins.any (n: n == llmLan) hosts;
    assert !(builtins.any (n: n == llmMesh) meshHosts);
    # SSH remains reachable through the firewall.
    assert builtins.elem 22 homelabConfig.networking.firewall.allowedTCPPorts;
    homelabConfig.system.build.toplevel;
}

