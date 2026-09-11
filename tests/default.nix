{ self, nixpkgs, impermanence, profiles, overlay }:

let
  pkgs = import nixpkgs {
    system = "x86_64-linux";
    overlays = [ overlay ];
  };
  exampleConfig = self.nixosConfigurations.example.config;
  homelabConfig = self.nixosConfigurations.mytecor-homelab.config;
  homelabRoutingRules = homelabConfig.lattice.llm-gateway.routingRules;
  homelabRulesWithAction = action:
    builtins.filter (rule: rule.action == action) homelabRoutingRules;
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
    assert homelabConfig.lattice.pi.enable;
    assert builtins.elem pkgs.lattice.pi homelabConfig.environment.systemPackages;
    # f8-02: Pi подключается к gateway только через логические классы, без
    # provider-specific discovery и без upstream credentials в конфиге.
    assert homelabConfig.lattice.pi.user == "root";
    assert homelabConfig.lattice.pi.settings.defaultProvider == "llm-gateway";
    assert homelabConfig.lattice.pi.settings.defaultModel == "standard";
    assert homelabConfig.lattice.pi.settings.defaultThinkingLevel == "xhigh";
    # pi-mcp-adapter: нода пинит Nix-сборку (pnpm builder) как extension-директорию.
    assert homelabConfig.lattice.pi.settings.extensions
      == [ pkgs.lattice.pi-mcp-adapter ];
    assert builtins.length (builtins.attrNames homelabConfig.lattice.pi.models) == 1;
    assert homelabConfig.lattice.pi.models.llm-gateway.baseUrl == "http://127.0.0.1:9208/v1";
    assert homelabConfig.lattice.pi.models.llm-gateway.api == "openai-completions";
    assert homelabConfig.lattice.pi.models.llm-gateway.discoverModels == false;
    # Несекретный placeholder вместо null: Pi резолвит модели провайдера только при
    # непустом apiKey (иначе session/new падает с authRequired). Реальный секрет —
    # только внутри llm-gateway; это значение не credential.
    assert homelabConfig.lattice.pi.models.llm-gateway.apiKey == "lattice-loopback-gateway";
    assert map (m: m.id) homelabConfig.lattice.pi.models.llm-gateway.models == [ "standard" "stupid" ];
    # f8-03: воспроизводимый tool profile — базовый контракт + расширение попадают
    # в systemPackages, контракт окружения фиксируется в /etc/pi.env.
    assert homelabConfig.lattice.pi.tools == [ ];
    assert builtins.hasAttr "pi.env" homelabConfig.environment.etc;
    assert nixpkgs.lib.hasInfix "GIT_CONFIG_NOSYSTEM=1"
      homelabConfig.environment.etc."pi.env".text;
    assert nixpkgs.lib.hasInfix "LANG=C.UTF-8"
      homelabConfig.environment.etc."pi.env".text;
    assert builtins.elem homelabConfig.lattice.pi.toolProfile
      homelabConfig.environment.systemPackages;
    # Активация материализует immutable JSON в store как симлинки ~/.pi/agent.
    assert builtins.hasAttr "pi-config" homelabConfig.system.activationScripts;
    assert nixpkgs.lib.hasInfix ".pi/agent"
      homelabConfig.system.activationScripts.pi-config.text;
    assert nixpkgs.lib.hasInfix "settings.json"
      homelabConfig.system.activationScripts.pi-config.text;
    assert nixpkgs.lib.hasInfix "models.json"
      homelabConfig.system.activationScripts.pi-config.text;
    assert nixpkgs.lib.hasInfix "ln -sfn"
      homelabConfig.system.activationScripts.pi-config.text;
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
    assert builtins.attrNames homelabConfig.lattice.llm-gateway.providers == [
      "dahl"
      "gonka-api"
      "gonka-openbroker"
      "gonka-proxy"
      "gonkarouter"
      "hyperfusion"
    ];
    assert nixpkgs.lib.all
      (provider: provider.modelsUrl == null)
      (builtins.attrValues homelabConfig.lattice.llm-gateway.providers);
    assert homelabConfig.lattice.llm-gateway.providers.gonka-proxy.inferenceUrl
      == "https://api.proxy.gonka.gg/v1";
    assert homelabConfig.lattice.llm-gateway.providers.gonka-api.inferenceUrl
      == "https://hskyauefqcgbvgvxkluj.supabase.co/functions/v1/gonka";
    # f7-12: the homelab routing table is a flat named-route graph. Provider
    # filters own selection, maps own only native IDs, and retry/fallback/hedge
    # point at explicit subroutes.
    assert !(builtins.hasAttr "models" homelabConfig.lattice.llm-gateway);
    assert nixpkgs.lib.all (rule: rule.route != "") homelabRoutingRules;
    assert builtins.length (homelabRulesWithAction "map") == 7;
    assert nixpkgs.lib.all
      (rule: rule.native != ""
        && !(builtins.hasAttr "providers" rule)
        && !(builtins.hasAttr "model" rule))
      (homelabRulesWithAction "map");
    # Entry/retry/hedge routes keep the base native; the standard fallback
    # route alone carries Hyperfusion's prefixed alias.
    assert nixpkgs.lib.any
      (rule: rule.route == "standard"
        && rule.native == "deepseek-ai/DeepSeek-V4-Flash-0731")
      (homelabRulesWithAction "map");
    assert nixpkgs.lib.any
      (rule: rule.route == "standard.fallback"
        && rule.native == "gonka/deepseek-ai/DeepSeek-V4-Flash-0731")
      (homelabRulesWithAction "map");
    assert nixpkgs.lib.any
      (rule: rule.route == "stupid"
        && rule.native == "MiniMaxAI/MiniMax-M2.7")
      (homelabRulesWithAction "map");
    # Two entry model filters, seven provider selections and three transition
    # error filters describe the graph without legacy match/map fields.
    assert builtins.length (homelabRulesWithAction "filter") == 12;
    assert builtins.length (builtins.filter
      (rule: builtins.hasAttr "model" rule.where)
      (homelabRulesWithAction "filter")) == 2;
    assert nixpkgs.lib.any
      (rule: (rule.where.model.eq or null) == "standard")
      (homelabRulesWithAction "filter");
    assert nixpkgs.lib.any
      (rule: (rule.where.model.eq or null) == "stupid")
      (homelabRulesWithAction "filter");
    assert builtins.length (builtins.filter
      (rule: builtins.hasAttr "provider" rule.where)
      (homelabRulesWithAction "filter")) == 7;
    assert nixpkgs.lib.all
      (rule:
        let selected = rule.where.provider."in" or [ ]; in
        selected != [ ] && builtins.length selected
          == builtins.length (nixpkgs.lib.unique selected))
      (builtins.filter
        (rule: builtins.hasAttr "provider" rule.where)
        (homelabRulesWithAction "filter"));
    assert builtins.length (builtins.filter
      (rule: builtins.hasAttr "provider" rule.where
        && (rule.where.provider.unused or false))
      (homelabRulesWithAction "filter")) == 4;
    assert builtins.length (builtins.filter
      (rule: builtins.hasAttr "error" rule.where)
      (homelabRulesWithAction "filter")) == 3;
    assert nixpkgs.lib.all
      (rule: !nixpkgs.lib.hasSuffix ".retry" rule.route
        || rule.where.error."in"
          == [ "429" "5xx" "timeout" "connection_error" "invalid_response" ])
      (builtins.filter
        (rule: builtins.hasAttr "error" rule.where)
        (homelabRulesWithAction "filter"));
    assert nixpkgs.lib.any
      (rule: rule.route == "standard.fallback"
        && builtins.elem "model_not_found" rule.where.error."in")
      (homelabRulesWithAction "filter");
    assert builtins.length (homelabRulesWithAction "rank") == 7;
    assert nixpkgs.lib.all
      (rule: rule.strategy == "priority")
      (homelabRulesWithAction "rank");
    assert builtins.length (homelabRulesWithAction "lease") == 2;
    assert nixpkgs.lib.all
      (rule: rule.source == "winner"
        && rule.duration == "10m"
        && rule.renewOnSuccess
        && rule.releaseOn == [ "429" "5xx" "timeout" "connection_error" ]
        && rule.releaseAfterSlowStarts == 3
        && rule.slowStart == "3s")
      (homelabRulesWithAction "lease");
    assert builtins.length (homelabRulesWithAction "affinity") == 2;
    assert nixpkgs.lib.all
      (rule: rule.sources
        == [ "responses.conversation" "responses.previous_response_id" ]
        && rule.ttl == "24h"
        && rule.onMissing == "ignore"
        && rule.onProviderFailure == "fail-closed")
      (homelabRulesWithAction "affinity");
    assert builtins.length (homelabRulesWithAction "race") == 7;
    assert nixpkgs.lib.all
      (rule: rule.count == (if builtins.elem rule.route [ "standard" "stupid" ] then 2 else 1))
      (homelabRulesWithAction "race");
    assert builtins.length (homelabRulesWithAction "retry") == 2;
    assert nixpkgs.lib.all
      (rule: rule.target == "${rule.route}.retry"
        && rule.attempts == 2
        && rule.backoffType == "exponential"
        && rule.backoffInitial == "200ms"
        && rule.backoffMax == "1s")
      (homelabRulesWithAction "retry");
    assert builtins.length (homelabRulesWithAction "hedge") == 2;
    assert nixpkgs.lib.all
      (rule: rule.after == "3s" && rule.target == "${rule.route}.hedge")
      (homelabRulesWithAction "hedge");
    assert builtins.length (homelabRulesWithAction "semaphore") == 2;
    assert nixpkgs.lib.all
      (rule: rule.maxCalls == 4
        && rule.maxInFlight == 3
        && rule.maxCallsPerProvider == 1)
      (homelabRulesWithAction "semaphore");
    assert builtins.length (homelabRulesWithAction "timeout") == 2;
    assert nixpkgs.lib.all
      (rule: rule.duration == "60s")
      (homelabRulesWithAction "timeout");
    assert builtins.length (homelabRulesWithAction "fallback") == 1;
    assert nixpkgs.lib.all
      (rule: rule.route == "standard" && rule.target == "standard.fallback")
      (homelabRulesWithAction "fallback");
    # Removed routing fields are absent from every typed rule.
    assert nixpkgs.lib.all
      (rule: nixpkgs.lib.all
        (field: !(builtins.hasAttr field rule))
        [ "model" "match" "providers" "scope" "on" "fallbackStrategy" "accessGroups" ])
      homelabRoutingRules;

    assert nixpkgs.lib.hasInfix "lattice-llm-gateway"
      homelabConfig.systemd.services.llm-gateway.serviceConfig.ExecStart;
    assert homelabConfig.systemd.services.llm-gateway.serviceConfig.RuntimeDirectoryPreserve
      == "restart";
    assert builtins.hasAttr "llm-gateway-mdns" homelabConfig.systemd.services;
    assert nixpkgs.lib.hasInfix "llm-gateway.mytecor-homelab.local"
      homelabConfig.systemd.services.llm-gateway-mdns.script;
    assert homelabConfig.networking.firewall.allowedTCPPorts == [ 22 80 ];
    assert nixpkgs.lib.hasInfix "--config /var/lib/rnsh --rnsconfig /var/lib/rns"
      homelabConfig.systemd.services.rnsh.serviceConfig.ExecStart;
    homelabConfig.system.build.toplevel;
}
