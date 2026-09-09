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
    assert builtins.length (builtins.attrNames homelabConfig.lattice.pi.models) == 1;
    assert homelabConfig.lattice.pi.models.llm-gateway.baseUrl == "http://127.0.0.1:9208/v1";
    assert homelabConfig.lattice.pi.models.llm-gateway.api == "openai-completions";
    assert homelabConfig.lattice.pi.models.llm-gateway.discoverModels == false;
    assert homelabConfig.lattice.pi.models.llm-gateway.apiKey == null;
    assert map (m: m.id) homelabConfig.lattice.pi.models.llm-gateway.models == [ "standard" "stupid" ];
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
    # f7-10: the homelab route is built exclusively from explicit map actions
    # (native -> provider ids); no access groups or separate models registry.
    assert !(builtins.hasAttr "models" homelabConfig.lattice.llm-gateway);
    assert builtins.length (builtins.filter
      (rule: rule.action == "map")
      homelabConfig.lattice.llm-gateway.routingRules) == 3;
    assert nixpkgs.lib.all
      (rule: rule.action != "map" || (rule.native != "" && builtins.length rule.providers >= 1))
      homelabConfig.lattice.llm-gateway.routingRules;
    # The primary maps bind the base native to every provider; the fallback
    # stage gives only hyperfusion the prefixed DeepSeek alias.
    assert nixpkgs.lib.any
      (rule: rule.action == "map" && rule.model == "standard"
        && rule.native == "deepseek-ai/DeepSeek-V4-Flash-0731"
        && builtins.length rule.providers == 6)
      homelabConfig.lattice.llm-gateway.routingRules;
    assert nixpkgs.lib.any
      (rule: rule.action == "map" && rule.model == "standard"
        && rule.native == "gonka/deepseek-ai/DeepSeek-V4-Flash-0731"
        && rule.providers == [ "hyperfusion" ])
      homelabConfig.lattice.llm-gateway.routingRules;
    assert nixpkgs.lib.any
      (rule: rule.action == "map" && rule.model == "stupid"
        && rule.native == "MiniMaxAI/MiniMax-M2.7"
        && builtins.length rule.providers == 6)
      homelabConfig.lattice.llm-gateway.routingRules;
    # One pending pool per provider: no provider appears twice in one stage.
    assert nixpkgs.lib.all
      (rule: rule.action != "map" || (builtins.length rule.providers
        == builtins.length (nixpkgs.lib.unique rule.providers)))
      homelabConfig.lattice.llm-gateway.routingRules;
    assert builtins.length (builtins.filter
      (rule: rule.action == "rank")
      homelabConfig.lattice.llm-gateway.routingRules) == 2;
    assert nixpkgs.lib.all
      (rule: rule.action != "rank" || rule.strategy == "priority")
      homelabConfig.lattice.llm-gateway.routingRules;
    assert builtins.length (builtins.filter
      (rule: rule.action == "lease")
      homelabConfig.lattice.llm-gateway.routingRules) == 2;
    assert nixpkgs.lib.all
      (rule: rule.action != "lease" || (rule.source == "winner"
        && rule.duration == "10m"
        && rule.renewOnSuccess
        && rule.releaseOn == [ "429" "5xx" "timeout" "connection_error" ]
        && rule.releaseAfterSlowStarts == 3
        && rule.slowStart == "3s"))
      homelabConfig.lattice.llm-gateway.routingRules;
    assert builtins.length (builtins.filter
      (rule: rule.action == "affinity")
      homelabConfig.lattice.llm-gateway.routingRules) == 2;
    assert nixpkgs.lib.all
      (rule: rule.action != "affinity" || (rule.sources
        == [ "responses.conversation" "responses.previous_response_id" ]
        && rule.ttl == "24h"
        && rule.onMissing == "ignore"
        && rule.onProviderFailure == "fail-closed"))
      homelabConfig.lattice.llm-gateway.routingRules;
    assert builtins.length (builtins.filter
      (rule: rule.action == "race")
      homelabConfig.lattice.llm-gateway.routingRules) == 2;
    assert nixpkgs.lib.all
      (rule: rule.action != "race" || rule.count == 2)
      homelabConfig.lattice.llm-gateway.routingRules;
    assert builtins.length (builtins.filter
      (rule: rule.action == "retry")
      homelabConfig.lattice.llm-gateway.routingRules) == 2;
    assert nixpkgs.lib.all
      (rule: rule.action != "retry" || (rule.scope == "next"
        && rule.count == 1
        && rule.attempts == 2
        && rule.on == [ "429" "5xx" "timeout" "connection_error" "invalid_response" ]
        && rule.backoffInitial == "200ms"
        && rule.backoffMax == "1s"))
      homelabConfig.lattice.llm-gateway.routingRules;
    assert builtins.length (builtins.filter
      (rule: rule.action == "hedge")
      homelabConfig.lattice.llm-gateway.routingRules) == 2;
    assert nixpkgs.lib.all
      (rule: rule.action != "hedge" || rule.after == "3s")
      homelabConfig.lattice.llm-gateway.routingRules;
    assert builtins.length (builtins.filter
      (rule: rule.action == "semaphore")
      homelabConfig.lattice.llm-gateway.routingRules) == 2;
    assert nixpkgs.lib.all
      (rule: rule.action != "semaphore" || (rule.maxCalls == 4
        && rule.maxInFlight == 3
        && rule.maxCallsPerProvider == 1))
      homelabConfig.lattice.llm-gateway.routingRules;
    assert builtins.length (builtins.filter
      (rule: rule.action == "timeout")
      homelabConfig.lattice.llm-gateway.routingRules) == 2;
    assert nixpkgs.lib.all
      (rule: rule.action != "timeout" || rule.duration == "60s")
      homelabConfig.lattice.llm-gateway.routingRules;
    # The fallback stage is declared exactly once (standard) and routes only
    # errors listed in its on filter, including model_not_found.
    assert builtins.length (builtins.filter
      (rule: rule.action == "fallback")
      homelabConfig.lattice.llm-gateway.routingRules) == 1;
    assert builtins.length (builtins.filter
      (rule: rule.action == "fallback" && rule.model == "standard"
        && rule.fallbackStrategy == "race"
        && builtins.elem "model_not_found" rule.on)
      homelabConfig.lattice.llm-gateway.routingRules) == 1;
    # No access_groups anywhere (the option was removed) and no separate
    # models list.
    assert nixpkgs.lib.all
      (rule: (rule.accessGroups or [ ]) == [ ])
      homelabConfig.lattice.llm-gateway.routingRules;

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
