{ config, lib, pkgs, ... }:

let
  rootPasswordHashFile = ./secrets/root-password-hash.age;
  hasRootPassword = builtins.pathExists rootPasswordHashFile;
  # Non-secret SSIDs exposed as world-readable store files, matching the module's
  # "both fields are file paths" contract. Passwords still come from a shared
  # agenix secret (wifi-password.age); only the home SSID uses wifi-ssid.age.
  ssidFile = name: value: "${pkgs.writeText "lattice-ssid-${name}" value}";
in
{
  networking.hostName = "mytecor-homelab";

  age = {
    identityPaths = [ "/persist/var/lib/lattice/age/identity" ];
    secrets = {
      wifi-ssid = {
        file = ./secrets/wifi-ssid.age;
        mode = "0400";
      };
      wifi-password = {
        file = ./secrets/wifi-password.age;
        mode = "0400";
      };
    } // lib.optionalAttrs hasRootPassword {
      root-password-hash = {
        file = rootPasswordHashFile;
        mode = "0400";
      };
    } // {
      radicle-private-key = {
        file = ./secrets/radicle-private-key.age;
        mode = "0400";
      };
      # LLM Gateway provider keys - generate via agenix before deployment
      llm-provider-gonka-gg-proxy = {
        file = ./secrets/llm-provider-gonka-gg-proxy.age;
        mode = "0400";
      };
      llm-provider-gonka-gg-openbroker = {
        file = ./secrets/llm-provider-gonka-gg-openbroker.age;
        mode = "0400";
      };
      llm-provider-gonka-api = {
        file = ./secrets/llm-provider-gonka-api.age;
        mode = "0400";
      };
      llm-provider-dahl = {
        file = ./secrets/llm-provider-dahl.age;
        mode = "0400";
      };
      llm-provider-hyperfusion = {
        file = ./secrets/llm-provider-hyperfusion.age;
        mode = "0400";
      };
      llm-provider-gonkarouter = {
        file = ./secrets/llm-provider-gonkarouter.age;
        mode = "0400";
      };
      # F12: Grafana admin password via agenix (file provider, never in store).
      grafana-admin-password = {
        file = ./secrets/grafana-admin-password.age;
        mode = "0400";
      };
      # F12: Grafana secret_key (NixOS 26.05 requires explicit value).
      grafana-secret-key = {
        file = ./secrets/grafana-secret-key.age;
        mode = "0400";
      };
    };
  };

  lattice.wireless.networks = [
    {
      # Home AP ("BNF Space"), strongest signal (100%). SSID kept in wifi-ssid.age.
      ssid = config.age.secrets.wifi-ssid.path;
      password = config.age.secrets.wifi-password.path;
      priority = 100;
    }
    {
      # Only roaming AP verified to be in the same subnet (192.168.60.0/24) as
      # the operator Mac (macbook.local reachable from it). R509/R606/R504/P 304
      # resolved to different subnets and were removed.
      ssid = ssidFile "r505" "R505";
      password = config.age.secrets.wifi-password.path;
      priority = 60;
    }
  ];

  # Route the attached rnsh service's announces and links through the public peers.
  lattice.rns-server.reticulum.enable_transport = true;

  # Pi подключается к gateway по loopback с только логическими классами. Provider-specific
  # discovery отключён; upstream credentials остаются внутри llm-gateway и сюда не попадают.
  lattice.pi = {
    enable = true;
    user = "root";
    settings = {
      defaultProvider = "llm-gateway";
      defaultModel = "standard";
      defaultThinkingLevel = "xhigh";
      # pi-mcp-adapter (https://pi.dev/packages/pi-mcp-adapter): доступ к MCP-серверам
      # через один proxy tool без раздувания контекста. Полная сборка через pnpm
      # builder (packages/pi-mcp-adapter, lock + store-path), загружается как
      # extension-директория из settings.extensions — на ноде не нужны ни node/npm,
      # ни runtime-загрузки из npm registry.
      # pi-retry (@geebos/pi-retry): классифицирует provider-specific/stalled-stream
      # ошибки как retryable. Паттерны ниже (RegExp) делают "upstream stream failed"
      # ретраябельным — llm-gateway шлёт это сообщение по SSE при обрыве апстрим-
      # стрима (5xx от hyperfusion), и без паттерна встроенный ретрай pi его не
      # ловит (см. packages/pi-retry/package.nix). "unsupported model" ретраить НЕ
      # нужно: модель не поддерживается — повторный запрос бессмысленен.
      extensions = [ pkgs.lattice.pi-mcp-adapter pkgs.lattice.pi-retry ];
      retry = [ "^Provider finish_reason: abort$" "upstream stream failed" ];
    };
    models.llm-gateway = {
      baseUrl = "http://127.0.0.1:9208/v1";
      api = "openai-completions";
      # Несекретный placeholder: gateway работает без client auth (clientCredentialFile не
      # задан) и игнорирует Bearer, но Pi считает провайдера пригодным только при непустом
      # apiKey — иначе список доступных моделей пуст и ACP session/new завершается
      # authRequired. Это не credential; реальные ключи остаются в agenix-секретах gateway.
      apiKey = "lattice-loopback-gateway";
      discoverModels = false;
      models = [
        { id = "standard"; }
        { id = "stupid"; }
        { id = "smart"; }
      ];
      modelOverrides = {
        standard = {
          # llm-gateway роутит standard на text-only DeepSeek-V4-Flash-0731; без
          # ["text"] Pi считает модель мультимодальной и шлёт image_url, от чего
          # upstream отвечает 400 (not multimodal / at most 5 images / 413 size).
          input = [ "text" ];
          thinkingLevelMap = {
            off = null; minimal = null; low = null; medium = null;
            high = null; xhigh = null; max = null;
          };
          compat = { supportsReasoningEffort = false; };
        };
        stupid = {
          input = [ "text" ];
          thinkingLevelMap = {
            off = null; minimal = null; low = null; medium = null;
            high = null; xhigh = null; max = null;
          };
          compat = { supportsReasoningEffort = false; };
        };
        # smart → zai-org/GLM-5.3-Flash via the llm-gateway. Multimodal
        # (vision verified), accepts the developer role, and always-reasoning:
        # GLM ignores a thinking:disabled toggle and reasons regardless. Pi's
        # zai thinkingFormat sends thinking:{type:enabled/disabled}; since no
        # reasoning level is controllable, none is exposed (all null keeps
        # getSupportedThinkingLevels empty) and input stays full multimodal.
        smart = {
          reasoning = true;
          input = [ "text" "image" ];
          thinkingLevelMap = {
            off = null; minimal = null; low = null; medium = null;
            high = null; xhigh = null; max = null;
          };
          compat = {
            supportsReasoningEffort = false;
            # GLM-5.3-Flash accepts the developer role and the zai thinking
            # shape; declare thinkingFormat explicitly so the model is treated
            # as reasoning-capable through the gateway passthrough.
            thinkingFormat = "zai";
          };
        };
      };
    };
  };

  # acp-normalizer (packages/acp-normalizer): Hydra transformer, который до broadcast
  # переприсваивает per-token messageId стабильным id на всё логическое ассистентское
  # сообщение. Clients, ключующие отрисовку по messageId (superlite), рвали ответы на
  # отдельные чанки; defaultTransformers применяет нормализатор ко всем сессиям без
  # участия клиента.
  lattice.pi-acp-daemon = {
    # f8-06 fix: the daemon's own systemd PATH has no `sh`, so spawned Pi
    # sessions got `spawn sh ENOENT` from the bash tool. Give every agent the
    # same bash/git/tools contract as the local runtime.
    path = [ pkgs.lattice.pi-tool-profile ];
    # TEMPORARY wide-open network/cap access (iw/ip/nl80211, sudo). This must
    # be reverted to the strict sandbox; see modules/pi-acp-daemon README note.
    privileged = true;
    transformers.acp-normalizer.command =
      [ "${pkgs.lattice.acp-normalizer}/bin/acp-normalizer" ];
    defaultTransformers = [ "acp-normalizer" ];
  };

  # f9-02: repo-scoped authorization. The cache proxy is a shared reader whose
  # origin fetch (comin-source-sync) follows the public `mytecor/lattice` repo;
  # the allowlist bounds it to exactly that repository. The repo is public and
  # fetched anonymously (no upstream credential service-side), so the module
  # assertion (credential ⇒ non-empty allowlist) holds trivially; if a private
  # origin is ever added, its per-repo credential goes with an explicit
  # allowRepos entry per KEY_MANAGEMENT.md.
  lattice.git-cache-proxy.allowRepos = [ "mytecor/lattice" ];

  # f9-03: Verdaccio npm caching proxy. Порт и остальные runtime-значения приходят
  # из cache-plane профиля (latticePorts.verdaccio = 9212 в ports.nix, host
  # 127.0.0.1, cacheRoot /var/cache/verdaccio). На ноде фиксируем только
  # enable = true; порт/хост/cacheRoot НЕ дублируются, чтобы не разошлись с
  # общим реестром портов. Никаких node-specific значений этой ноде не нужно:
  # cache-only, loopback, upstream registry.npmjs.org.
  lattice.verdaccio.enable = true;

  # LLM Gateway: flat routing_rules with named routes. Every rule belongs to a
  # named route; a filter with where.model makes the route the entry route for
  # a logical model, filter provider builds the provider selection, map binds it
  # to one native model, rank orders the pool, balance selects a provider on
  # runtime (round_robin), affinity pins stateful chains, race defines the
  # parallel batch, and retry/hedge are explicit transitions to named subroutes
  # (standard.retry, standard.hedge) whose own error/provider filters decide
  # when they apply. One request never creates more than four upstream calls
  # (race 1 + one hedged target, or race 1 + two retries of one target), more
  # than three concurrent calls, or a repeated call to one provider (the unused
  # provider routing policy). Provider transport and credentials stay in the
  # registry.
  lattice.llm-gateway = {
    # Debug logs contain routing metadata and sanitized upstream errors, never prompts or keys.
    logLevel = "debug";
    providers = {
      gonka-proxy = {
        id = "gonka-proxy";
        inferenceUrl = "https://api.proxy.gonka.gg/v1";
        apiKeyFile = config.age.secrets.llm-provider-gonka-gg-proxy.path;
        priority = 50;
      };
      gonka-openbroker = {
        id = "gonka-openbroker";
        inferenceUrl = "https://api.openbroker.gonka.gg/v1";
        apiKeyFile = config.age.secrets.llm-provider-gonka-gg-openbroker.path;
        priority = 40;
      };
      gonka-api = {
        id = "gonka-api";
        inferenceUrl = "https://hskyauefqcgbvgvxkluj.supabase.co/functions/v1/gonka";
        apiKeyFile = config.age.secrets.llm-provider-gonka-api.path;
        priority = 30;
      };
      dahl = {
        id = "dahl";
        inferenceUrl = "https://inference.dahl.global/v1";
        apiKeyFile = config.age.secrets.llm-provider-dahl.path;
        priority = 20;
      };
      hyperfusion = {
        id = "hyperfusion";
        inferenceUrl = "https://api.hyperfusion.io/v1";
        apiKeyFile = config.age.secrets.llm-provider-hyperfusion.path;
        priority = 100;
      };
      gonkarouter = {
        id = "gonkarouter";
        inferenceUrl = "https://api.gonkarouter.io/v1";
        apiKeyFile = config.age.secrets.llm-provider-gonkarouter.path;
        priority = 10;
      };
    };
    # The entry route maps one native per logical model to a provider set; the
    # retry and hedge subroutes re-select unused providers. `standard`,
    # `stupid` and `smart` all use the full provider universe: providers whose
    # catalog does not yet serve the native ID fail exact validation locally
    # (model_not_found, no upstream call) and are skipped, so `smart`
    # (GLM-5.3-Flash) rides whatever subset of the networks already carry it.
    # The fallback subroute for `standard` gives Hyperfusion its second catalog
    # alias so a model_not_found in the primary alias can fail over to the
    # prefixed native Hyperfusion actually serves.
    #
    # f7-13: live run of provider balancing. `balance` replaces `lease` as the
    # runtime selection step before `race`, so traffic distributes across
    # healthy providers instead of concentrating on the fastest lease holder.
    # Distribution requires `race count = 1` (deterministic selection); lease
    # and balance are mutually exclusive on one route. Live run showed adaptive
    # with even flat weights still re-concentrates ~25/27 requests on
    # hyperfusion, because the EWMA latency factor (min latency / latency)
    # dominates weight × health — adaptive is designed to favour the best
    # performer. round_robin is the documented max-distribution strategy: it
    # rotates over all healthy candidates (health floor via errorBudget,
    # default 5m/0.2) and excludes unhealthy or blacklisted ones, which
    # satisfies both DoD 1 (distribution) and DoD 2 (unhealthy excluded).
    # window/errorBudget use the module defaults.
    routingRules = let
      allProviders = [
        "gonka-proxy"
        "gonka-openbroker"
        "gonka-api"
        "dahl"
        "hyperfusion"
        "gonkarouter"
      ];
      # The bounded primary pipeline used by every logical model. All models
      # select from the full enabled provider universe; providers whose catalog
      # does not yet serve the native ID fail exact validation with
      # model_not_found locally (no upstream call) and are simply skipped — the
      # healthy carriers take the traffic. This keeps `smart` on the same
      # provider set as standard/stupid as each network rolls out GLM-5.3-Flash
      # on its side.
      primaryRules = model: native: [
        { route = model; action = "filter"; where = { model = { eq = model; }; }; }
        { route = model; action = "filter"; where = { provider = { "in" = allProviders; }; }; }
        { route = model; action = "map"; native = native; }
        { route = model; action = "rank"; strategy = "priority"; }
        {
          route = model;
          action = "balance";
          strategy = "round_robin";
        }
        {
          route = model;
          action = "affinity";
          sources = [ "responses.conversation" "responses.previous_response_id" ];
          ttl = "24h";
          onMissing = "ignore";
          onProviderFailure = "fail-closed";
        }
        { route = model; action = "race"; count = 1; }
        {
          route = model;
          action = "retry";
          target = "${model}.retry";
          attempts = 2;
          backoffType = "exponential";
          backoffInitial = "200ms";
          backoffMax = "1s";
        }
        { route = model; action = "hedge"; after = "3s"; target = "${model}.hedge"; }
        {
          route = model;
          action = "semaphore";
          maxCalls = 4;
          maxInFlight = 3;
          maxCallsPerProvider = 1;
        }
        { route = model; action = "timeout"; duration = "60s"; }
        # Retry subroute: applies only to the listed failures and re-selects
        # unused providers, one target per retry entry.
        {
          route = "${model}.retry";
          action = "filter";
          where = {
            error = { "in" = [ "429" "5xx" "timeout" "connection_error" "invalid_response" ]; };
          };
        }
        {
          route = "${model}.retry";
          action = "filter";
          where = { provider = { "in" = allProviders; unused = true; }; };
        }
        { route = "${model}.retry"; action = "map"; native = native; }
        { route = "${model}.retry"; action = "rank"; strategy = "priority"; }
        { route = "${model}.retry"; action = "race"; count = 1; }
        # Hedge subroute: latency alternative that races one unused target.
        {
          route = "${model}.hedge";
          action = "filter";
          where = { provider = { "in" = allProviders; unused = true; }; };
        }
        { route = "${model}.hedge"; action = "map"; native = native; }
        { route = "${model}.hedge"; action = "rank"; strategy = "priority"; }
        { route = "${model}.hedge"; action = "race"; count = 1; }
      ];
      # Fallback subroute for standard: Hyperfusion accepts both catalog aliases.
      standardFallback = [
        {
          route = "standard";
          action = "fallback";
          target = "standard.fallback";
        }
        {
          route = "standard.fallback";
          action = "filter";
          where = {
            error = { "in" = [ "model_not_found" "429" "5xx" "timeout" "connection_error" ]; };
          };
        }
        {
          route = "standard.fallback";
          action = "filter";
          where = { provider = { "in" = [ "hyperfusion" ]; }; };
        }
        {
          route = "standard.fallback";
          action = "map";
          native = "gonka/deepseek-ai/DeepSeek-V4-Flash-0731";
        }
        { route = "standard.fallback"; action = "rank"; strategy = "priority"; }
        { route = "standard.fallback"; action = "race"; count = 1; }
      ];
      # Fallback subroute for smart (GLM-5.3-Flash): re-selects one of the
      # providers that actually carry the exact unprefixed native. The entry
      # route chooses one provider by round_robin from the full universe, and
      # when that provider rejects GLM (upstream 404, or a local model_not_found
      # because its catalog does not yet carry the exact ID) the request must
      # hand off to a carrier instead of failing. The recovery needs more than
      # a retry: the retry subroute's error filter excludes 404/model_not_found
      # and the hedge is latency-only (abandoned on a fast terminal failure), so
      # an explicit fallback is the only transition that rescues a fast 404. The
      # fallback races the whole exact-ID carrier pool (count 0 = whole pool) so
      # a stale catalog or a second 404 on one carrier does not strand the
      # request; the unused policy skips a carrier that already served this
      # request. Hyperfusion is intentionally absent: it serves GLM only under
      # the prefixed gonka/ alias (see standardFallback).
      smartFallback = [
        {
          route = "smart";
          action = "fallback";
          target = "smart.fallback";
        }
        {
          route = "smart.fallback";
          action = "filter";
          where = {
            error = { "in" = [ "404" "model_not_found" "429" "5xx" "timeout" "connection_error" "invalid_response" ]; };
          };
        }
        {
          route = "smart.fallback";
          action = "filter";
          where = { provider = { "in" = [ "gonka-proxy" "gonka-openbroker" "gonkarouter" ]; unused = true; }; };
        }
        {
          route = "smart.fallback";
          action = "map";
          native = "zai-org/GLM-5.3-Flash";
        }
        { route = "smart.fallback"; action = "rank"; strategy = "priority"; }
        { route = "smart.fallback"; action = "race"; count = 0; }
      ];
    in
    # A provider whose catalog lists GLM but that fails the upstream call with
    # a provider-side 404 must not leave `smart` without a carrier: that 404 is
    # exact-match (catalog present, model absent for the proxy) and is not on
    # the plain 404 retry list, so the only way to recover is an explicit
    # fallback that re-selects an unused carrier provider. The fallback also
    # absorbs a local model_not_found (catalog snapshot not yet rolled out on
    # the selected provider) so the request still reaches a carrier.
    primaryRules "stupid" "MiniMaxAI/MiniMax-M2.7"
    ++ primaryRules "standard" "deepseek-ai/DeepSeek-V4-Flash-0731"
    ++ primaryRules "smart" "zai-org/GLM-5.3-Flash"
    ++ standardFallback
    ++ smartFallback;
  };

  # F12 observability: Grafana admin password comes from an agenix secret via
  # the file provider (never plaintext in the store). Datasources (Prometheus +
  # Loki) and dashboard provisioning are configured by the module; only the
  # secret is node-specific. Everything binds 127.0.0.1 (non-public).
  lattice.grafana = {
    adminPasswordFile = config.age.secrets.grafana-admin-password.path;
    secretKeyFile = config.age.secrets.grafana-secret-key.path;
  };

  lattice.rnsh = {
    # Public hash only; private operator identity stays on the Mac in .secrets/rnsh-operator.
    allowed = [ "59bfffc440ddc304749fd9477865b811" ];
    command = [ "/run/current-system/sw/bin/bash" ];
  };

  # This public key belongs only to the unattended Radicle service identity.
  services.radicle.publicKey =
    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPTgXojRWDf3RXhVEILTxI/T9lfL0S6W9cHscze5wszj";

  services.openssh = {
    enable = true;
    openFirewall = true;
    hostKeys = [
      {
        path = "/persist/etc/ssh/ssh_host_ed25519_key";
        type = "ed25519";
      }
    ];
    settings = {
      KbdInteractiveAuthentication = false;
      PasswordAuthentication = false;
      PermitRootLogin = "prohibit-password";
    };
  };

  users.mutableUsers = false;
  users.users.root = {
    openssh.authorizedKeys.keys = [
      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP6Gm4DbPs1Ar7/g9IU90YS873SoMYMQhc0xjQFHtJEk mytecor@macbook.local"
    ];
  } // (if hasRootPassword then {
    hashedPasswordFile = config.age.secrets.root-password-hash.path;
  } else {
    hashedPassword = "*";
  });

  services.avahi = {
    enable = true;
    nssmdns4 = true;
    publish = {
      enable = true;
      addresses = true;
      workstation = true;
    };
  };

  environment.persistence."/persist".directories = [
    "/var/lib/comin"
    { directory = "/var/lib/rns"; user = "rns"; group = "rns"; mode = "0750"; }
    { directory = "/var/lib/rnsh"; user = "rnsh"; group = "rnsh"; mode = "0700"; }
    { directory = "/var/lib/radicle"; user = "radicle"; group = "radicle"; mode = "0750"; }
    { directory = "/var/lib/hydra-acp"; user = "root"; group = "root"; mode = "0700"; }
    # F12 observability data survives reboots (impermanence).
    { directory = "/var/lib/prometheus"; user = "prometheus"; group = "prometheus"; mode = "0750"; }
    { directory = "/var/lib/loki"; user = "loki"; group = "loki"; mode = "0750"; }
    { directory = "/var/lib/grafana"; user = "grafana"; group = "grafana"; mode = "0750"; }
  ];

  system.stateVersion = "26.05";
}
