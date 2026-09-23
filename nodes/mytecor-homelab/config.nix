{ config, lib, pkgs, ... }:

let
  rootPasswordHashFile = ./secrets/root-password-hash.age;
  hasRootPassword = builtins.pathExists rootPasswordHashFile;
  # f4-05: Cloudflare API token для DNS-01 (валидация ACME через публичный DNS).
  # Секрет создаёт оператор (см. nodes/mytecor-homelab/README.md); пока .age-файла нет,
  # mesh-HTTPS остаётся выключен, LAN-контракт *.local не затронут.
  caddyCloudflareTokenFile = ./secrets/caddy-cloudflare-token.age;
  hasCaddyCloudflare = builtins.pathExists caddyCloudflareTokenFile;
  # Non-secret SSIDs exposed as world-readable store files, matching the module's
  # "both fields are file paths" contract. Passwords still come from a shared
  # agenix secret (wifi-password.age); only the home SSID uses wifi-ssid.age.
  ssidFile = name: value: "${pkgs.writeText "lattice-ssid-${name}" value}";
  # zai `thinking` control rejected by generic OpenAI-compatible upstreams
  # (hyperfusion/litellm 400). Stripped for every provider so reasoning level
  # choice stays a native-model concern and no provider fails a race over an
  # unsupported request parameter.
  stripReasoningParams = [ "thinking" "reasoning_effort" ];
  # Gateways serving OpenAI-compatible models natively (the gonka carriers)
  # accept the zai `thinking` control. For these we don't just strip it: we
  # force `thinking:{"type":"disabled"}` on every request so reasoning is
  # hard-off for all models (standard/stupid DeepSeek-V4-Flash & MiniMax, and
  # smart GLM which — always-reasoning — ignores the toggle anyway). Applied
  # after strip_params, so the forced value wins even over a client that asked
  # for reasoning. hyperfusion/dahl/gonkarouter stay strip-only: litellm
  # rejects the zai `thinking` key with 400, and the others are untested.
  forceReasoningOff = {
    thinking = { type = "disabled"; };
  };
  # f4-05: внешний mesh-домен (aaa-записи *.homelab.myt.su → yggdrasil-адрес ноды).
  # Совпадает с lattice.tcp-gateway.meshDomain; используется для внешнего URL Grafana
  # и OIDC-контрактов Authentik (mesh-canonical, см. lattice.grafana ниже).
  meshDomain = "homelab.myt.su";
  # f18-08: Jev API keys (optional). Модуль управляет agenix-секретами неявно
  # (nullOr path); здесь нода определяет runtime-path, если .age-файл создан
  # оператором. Пока файла нет — секреты не подключаются, сервис стартует в
  # inspector-режиме без задач модели.
  jevTypesafeApiKeyFile = ./secrets/jev-typesafe-api-key.age;
  hasJevTypesafeKey = builtins.pathExists jevTypesafeApiKeyFile;
  jevTextModelApiKeyFile = ./secrets/jev-text-model-api-key.age;
  hasJevTextModelKey = builtins.pathExists jevTextModelApiKeyFile;
in
{
  networking.hostName = "mytecor-homelab";

  # f4-05: Yggdrasil — IPv6 mesh-оверлей для внешнего доступа к сервисам без белого IP.
  # Нода получает стабильный адрес в 200::/7 из agenix-ключа (приватный ключ из
  # yggdrasil-keys.age; PrivateKeyPath загружается через systemd credentials, в store
  # ключ не попадает — на это есть assertion модуля). Peers — проверенные на живом
  # клиенте (Mac) публичные ноды. Из этого адреса выпускаются поддомены homelab.myt.su
  # (см. README: внешний DNS *.homelab.myt.su → address).
  services.yggdrasil = {
    enable = true;
    settings = {
      # Стабильные, проверенные публичные peers (несколько регионов для отказоустойчивости).
      Peers = [
        "tls://ygg5.mk16.de:1338"
        "tls://supanadit.com:15021"
        "tls://ins.8px.sk:4321"
        "quic://asia.deinfra.org:15015"
        "tls://ygg-msk-1.averyan.ru:8362"
      ];
      PrivateKeyPath = config.age.secrets.yggdrasil-keys.path;
      IfName = "ygg0";
    };
  };

  # f4-05: внешний (mesh) ingress поверх LAN-контракта. Caddy обслуживает сервисы
  # по Host заголовку и для *.homelab.myt.su параллельно *.local. domain null → mesh закрыт.
  # Grafana выпускается на mesh (https://grafana.homelab.myt.su) — доступ через ygg
  # закрыт Authentik SSO (F14, нативный OIDC). llm-gateway остаётся только на
  # LAN-контракте *.local (нет публичной TLS/API-key защиты — извне недоступен).
  lattice.tcp-gateway = {
    meshDomain = "homelab.myt.su";
    meshExclude = [ "llm-gateway" ];
    # Cloudflare DNS-01: включается автоматически, как только оператор создаст секрет.
    cloudflareToken = lib.mkIf hasCaddyCloudflare config.age.secrets.caddy-cloudflare-token.path;
  };

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
      llm-provider-dahl-2 = {
        file = ./secrets/llm-provider-dahl-2.age;
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
        owner = "grafana";
        mode = "0400";
      };
      # F12: Grafana secret_key (NixOS 26.05 requires explicit value).
      grafana-secret-key = {
        file = ./secrets/grafana-secret-key.age;
        owner = "grafana";
        mode = "0400";
      };
      # F14: Grafana OAuth2 client secret for SSO login via Authentik (OIDC).
      grafana-oauth-client-secret = {
        file = ./secrets/grafana-oauth-client-secret.age;
        group = "authentik-oidc-secrets";
        mode = "0440";
      };
      # F14: Authentik secrets. Each file is a single `AUTHENTIK_*=...` line,
      # loaded by the authentik systemd units as EnvironmentFile (never in store).
      authentik-secret-key = {
        file = ./secrets/authentik-secret-key.age;
        mode = "0400";
      };
      authentik-bootstrap-token = {
        file = ./secrets/authentik-bootstrap-token.age;
        mode = "0400";
      };
      authentik-bootstrap-user = {
        file = ./secrets/authentik-bootstrap-user.age;
        mode = "0400";
      };
      authentik-bootstrap-email = {
        file = ./secrets/authentik-bootstrap-email.age;
        mode = "0400";
      };
      authentik-bootstrap-password = {
        file = ./secrets/authentik-bootstrap-password.age;
        mode = "0400";
      };
      # f4-05: стабильная идентичность ноды Yggdrasil (PKCS8 PEM private key — формат,
      # который требует PrivateKeyPath, см. src/config/config.go "...in PEM format").
      # Адрес в 200::/7 выводится из этого ключа и должен переживать перезагрузки — поэтому
      # ключ в agenix, а не генерируется заново при старте. При ротации ключа сервис
      # перезапускают вручную (в этой версии agenix нет restartUnits; секрет перечитывается
      # на следующей активации, yggdrasil подхватит новый адрес после перезапуска юнита).
      yggdrasil-keys = {
        file = ./secrets/yggdrasil-keys.age;
        mode = "0400";
      };
    } // lib.optionalAttrs hasJevTypesafeKey {
      # f18-08: Jev TYPESAFE_API_KEY (optional, agenix). Регистрируется только
      # когда оператор создал .age-файл; иначе `file`-путь не существует и eval
      # упал бы. Модуль монтирует ключ через LoadCredential, в store не попадает.
      jev-typesafe-api-key = {
        file = jevTypesafeApiKeyFile;
        mode = "0400";
      };
    } // lib.optionalAttrs hasJevTextModelKey {
      # f18-08: Jev TEXT_MODEL_API_KEY (optional, agenix). Смотри выше.
      jev-text-model-api-key = {
        file = jevTextModelApiKeyFile;
        mode = "0400";
      };
    } // lib.optionalAttrs hasCaddyCloudflare {
      # f4-05: токен Cloudflare для DNS-01 (acme_dns). Подаётся через systemd
      # EnvironmentFile (services.caddy.environmentFile), в store не попадает.
      caddy-cloudflare-token = {
        file = caddyCloudflareTokenFile;
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
        # smart → zai-org/GLM-5.3-Flash via the llm-gateway. Hyperfusion serves
        # the same GLM under a second catalog alias: the per-provider mapping
        # gives it gonka/zai-org/GLM-5.3-Flash directly in the entry pipeline,
        # not through a fallback — hyperfusion carries smart on its own native.
        # Multimodal
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
    # f15-01: ACP sessions open in the node's Lattice working checkout (see
    # profiles/node-dev), so an agent can edit, commit and push `main` from
    # the node itself.
    defaultCwd = "/var/lib/lattice-workspace/lattice";
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

  # f18-08: Jev API keys — runtime paths from agenix, wired conditionally (node
  # convention). Модуль монтирует их через LoadCredential; пока оператор не
  # создал .age-файл, пути null и сервис работает в inspector-режиме.
  lattice.jev-ultrafast = {
    typesafeApiKeyFile = if hasJevTypesafeKey then config.age.secrets.jev-typesafe-api-key.path else null;
    textModelApiKeyFile = if hasJevTextModelKey then config.age.secrets.jev-text-model-api-key.path else null;
  };

  # LLM Gateway: f7-14 declarative sugar. `models` generates the canonical
  # bounded pipeline for every logical model (filter model → filter provider →
  # map → rank → balance → affinity → race → retry → semaphore → timeout plus
  # the <model>.retry subroute); `pipeline` holds the deployment defaults and
  # per-model `pipeline` overrides specialize one model. The provider universe
  # (all enabled providers) is resolved from the provider registry, so a new
  # provider starts carrying traffic without touching the rules. Providers whose
  # catalog does not serve a native ID fail exact validation locally
  # (model_not_found, no upstream call) and are skipped.
  #
  # Balance is p2c (power of two choices, f7-14): two random healthy candidates
  # are drawn and the one with fewer in-flight branches wins, so load spreads
  # under concurrency without latency feedback. f7-13 showed that
  # latency-weighted selection (adaptive) and priority-derived weights both
  # re-concentrate on the fastest provider; p2c's in-flight signal is the
  # missing distribution mechanism. round_robin/adaptive/weighted remain
  # available as pipeline.balance.strategy overrides. Hedge is enabled at
  # 20s (see the pipeline block below) as a selection-phase safety net, with
  # the f7-13 caveat kept in mind: an aggressive `hedge after 3s` re-concentrates
  # completions (~2/3 on the fastest provider) even with a distributed primary
  # choice — 20s is deliberately outside the healthy-response head of the
  # distribution, so healthy requests never trigger it and only a genuinely
  # mute upstream pays the second call.
  # One request never creates more than five upstream calls (race 1 + hedge 1
  # + two retries + fallback), more than three concurrent ones, or a repeated
  # call to one provider (the unused provider policy restricts retry/hedge/
  # fallback; smart overrides to 6/4 with race count 0 (see below).
  lattice.llm-gateway = {
    # Debug logs contain routing metadata and sanitized upstream errors, never prompts or keys.
    logLevel = "debug";
    # Mid-stream 5xx breaks (hyperfusion, 2026-09-16 regression) feed the
    # gateway's cooldown/health/lease machinery, so a provider that repeatedly
    # breaks streams stops winning races; the pi-retry client fallback stays
    # as the last line of defense. The idle watchdog re-arms on every event,
    # so the explicit default only bounds a fully silent stream.
    streamIdleTimeout = "5m";
    providers = {
      gonka-proxy = {
        id = "gonka-proxy";
        inferenceUrl = "https://api.proxy.gonka.gg/v1";
        apiKeyFile = config.age.secrets.llm-provider-gonka-gg-proxy.path;
        priority = 50;
        stripParams = stripReasoningParams;
        setParams = forceReasoningOff;
      };
      gonka-openbroker = {
        id = "gonka-openbroker";
        inferenceUrl = "https://api.openbroker.gonka.gg/v1";
        apiKeyFile = config.age.secrets.llm-provider-gonka-gg-openbroker.path;
        priority = 40;
        stripParams = stripReasoningParams;
        setParams = forceReasoningOff;
      };
      gonka-api = {
        id = "gonka-api";
        inferenceUrl = "https://hskyauefqcgbvgvxkluj.supabase.co/functions/v1/gonka";
        apiKeyFile = config.age.secrets.llm-provider-gonka-api.path;
        priority = 30;
        stripParams = stripReasoningParams;
        setParams = forceReasoningOff;
      };
      dahl = {
        id = "dahl";
        inferenceUrl = "https://inference.dahl.global/v1";
        apiKeyFile = config.age.secrets.llm-provider-dahl.path;
        priority = 20;
        stripParams = stripReasoningParams;
      };
      dahl-2 = {
        id = "dahl-2";
        inferenceUrl = "https://inference.dahl.global/v1";
        apiKeyFile = config.age.secrets.llm-provider-dahl-2.path;
        priority = 20;
        stripParams = stripReasoningParams;
      };
      hyperfusion = {
        id = "hyperfusion";
        inferenceUrl = "https://api.hyperfusion.io/v1";
        apiKeyFile = config.age.secrets.llm-provider-hyperfusion.path;
        priority = 100;
        stripParams = stripReasoningParams;
      };
      gonkarouter = {
        id = "gonkarouter";
        inferenceUrl = "https://api.gonkarouter.io/v1";
        apiKeyFile = config.age.secrets.llm-provider-gonkarouter.path;
        priority = 10;
        stripParams = stripReasoningParams;
      };
    };
    # Pipeline defaults equal the built-in ones (providers = all enabled,
    # balance p2c with equal weights, race 1, retry 2 exponential, hedge
    # enabled 20s, semaphore 4/3/1, timeout 60s, affinity 24h) plus the
    # in-gateway stream takeover (`continue`) enabled for EVERY model: any
    # winner that relays content and then stalls (silence > idle) or closes
    # without finish_reason is continued on another provider with the partial
    # output reshared, instead of surfacing "Stream ended without
    # finish_reason" to the client
    # (observed across smart/standard/stupid, not only GLM reasoning). The
    # broken provider still enters cooldown/health. Per-model overrides remain
    # possible through models.<name>.pipeline.continue.
    pipeline = {
      continue = {
        enable = true;
        # idle lowered from the inherited 90s to 30s (2026-09-18): the GLM
        # smart route kept burning ~5min in dead upstream streams that relayed
        # keep-alives but never delivered content (empty_completion /
        # missing_finish_reason at ~300s, see session 01a0b3c1). The idle
        # takeover only reacts to real upstream silence (any event re-arms it),
        # so 30s is a safe lower bound — a healthy stream emits a delta or a
        # keep-alive well within 30s, and a truly dead one gets re-dispatched
        # three times faster. The global streamIdleTimeout (still 5m) is
        # shadowed by this value while continue is enabled, so leave it alone.
        idle = "30s";
        reshare = "full";
        # Chain retry budget: when every provider in the pool has already broken
        # during the request (takeover finds no eligible target left), the
        # gateway re-dispatches the WHOLE chain from the top with the accumulated
        # partial output reshared, instead of surfacing a terminal 504 and
        # breaking the client stream. Default is 0 (surface terminal error);
        # without it a fully-exhausted pool left the turn on "504 Gateway
        # Timeout" for the pi client to retry externally. 2 gives a dead pool
        # two fresh passes (cooldown gates the just-broken providers, so the
        # retry re-races the survivors). Bounded by maxContinueChainRetries (10).
        retries = 2;
      };
      # Selection-phase hedge (opt-in): a primary race winner that produces no
      # meaningful token within `after` starts the <model>.hedge subroute in
      # parallel (race count 1 more provider, rank priority), so a mute/hung
      # upstream no longer consumes the whole 60s route deadline alone — the
      # observed 30/32 selection 504s on `standard` were exactly this: race 1
      # on a concurrency-capped DeepSeek channel (429) sitting silent until the
      # global deadline, retry/fallback having no room. 20s < 60s leaves the
      # hedge winner time to deliver its first token. The hedge subroute's
      # unused policy means it only launches providers not already raced by
      # the entry route, so the pool for retry/fallback survives; on `smart`
      # (raceCount=0) the whole pool is already used, so the hedge finds an
      # empty unused set and launches nothing (harmless).
      hedge = {
        enable = true;
        after = "20s";
      };
    };
    models = {
      stupid.native = "MiniMaxAI/MiniMax-M2.7";
      standard.native = "deepseek-ai/DeepSeek-V4-Flash-0731";
      # smart (GLM-5.3-Flash): GLM carriers can hang for a long time while
      # reasoning without sending a meaningful token, and the default pace
      # (race count 1) once consumed the entire 60s route deadline on one hung
      # provider — the deadline is global, so retry/fallback got no room and
      # the request returned 504. The override restores the f7-13 topology:
      # race count 0 races the whole provider universe in parallel (carriers
      # without the exact GLM native pre-fail locally and are skipped), and
      # maxInFlight 4 launches all carriers concurrently. The 20s hedge stays
      # inert here: race count 0 already launches the whole pool at t=0, so by
      # the time the hedge fires every carrier is `used` and the unused filter
      # parses to an empty batch — harmless, and the pool for retry/fallback
      # survives.
      #
      smart = {
        native = "zai-org/GLM-5.3-Flash";
        nativeByProvider = { hyperfusion = "gonka/zai-org/GLM-5.3-Flash"; };
        # The historical 4096-token completion cap on dahl/gonkarouter is
        # absorbed by the continue rule (idle 30s + wait 10m): a winner that
        # truncates at 4096 and goes silent is continued on another provider
        # with the partial output reshared. All carriers serve the exact GLM
        # native (live /models check), so keep the full universe in the smart
        # pool.
        pipeline = {
          providers = [
            "gonka-proxy"
            "gonka-openbroker"
            "gonka-api"
            "dahl"
            "dahl-2"
            "hyperfusion"
            "gonkarouter"
          ];
          raceCount = 0;
          semaphore = {
            maxCalls = 6;
            maxInFlight = 4;
          };
        };
      };
    };
    # Escape hatch: raw rules appended after the generated pipelines. Only
    # fallbacks live here (the sugar owns everything else).
    #
    # Every fallback carries the SAME failure-class set
    # (fallbackErrorClasses): the full retryable universe, including both a
    # live upstream 404 ("404") and the catalog pre-dispatch rejection
    # ("model_not_found"). The standard route once omitted "404" (2026-09-20,
    # session 01a0bd52): gonka-proxy dropped DeepSeek-V4-Flash-0731 from its
    # serving pool while still advertising it in /models, the upstream returned
    # a live HTTP 404 (class "404", not "model_not_found"), neither retry nor
    # fallback matched, and the terminal 404 was surfaced verbatim to the
    # client. The generated <model>.retry subroutes (sugar) now share the same
    # classes, so a carrier that 404s is re-raced against unused providers and
    # the failing (provider, native) pair is cooldown-gated.
    routingRules =
      let
        # The one error set every fallback transition accepts. Mirrors
        # allRetryableClasses() plus the catalog model_not_found.
        fallbackErrorClasses = [
          "404"
          "model_not_found"
          "429"
          "5xx"
          "timeout"
          "connection_error"
          "invalid_response"
        ];
        allProviders = [
          "gonka-proxy"
          "gonka-openbroker"
          "gonka-api"
          "dahl"
          "dahl-2"
          "hyperfusion"
          "gonkarouter"
        ];
        # Smart fallback covers the whole universe too: continue absorbs the
        # historical 4096-cap carriers, so nothing is excluded here.
        smartExcluded = [];
      in
      [
        # standard: Hyperfusion's second catalog alias. Narrow on purpose — the
        # entry+retry chain (now 404-aware via the sugar) already re-races all
        # plain-deepseek carriers, so this net only gives Hyperfusion its
        # gonka/-prefixed native for the model_not_found/404 case.
        {
          route = "standard";
          action = "fallback";
          target = "standard.fallback";
        }
        {
          route = "standard.fallback";
          action = "filter";
          where = { error = { "in" = fallbackErrorClasses; }; };
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
      ]
      ++ [
        # stupid: universe-wide safety net (all providers serve the plain
        # MiniMax native, no capability exclusion needed), mirrored from smart
        # so every model has a uniform generic fallback.
        {
          route = "stupid";
          action = "fallback";
          target = "stupid.fallback";
        }
        {
          route = "stupid.fallback";
          action = "filter";
          where = { error = { "in" = fallbackErrorClasses; }; };
        }
        {
          route = "stupid.fallback";
          action = "filter";
          where = { provider = { "in" = allProviders; unused = true; }; };
        }
        {
          route = "stupid.fallback";
          action = "map";
          native = "MiniMaxAI/MiniMax-M2.7";
        }
        { route = "stupid.fallback"; action = "rank"; strategy = "priority"; }
        { route = "stupid.fallback"; action = "race"; count = 0; }
      ]
      ++ [
        # smart (GLM): generic universe-wide safety net (unused, race 0) for
        # the 404/model_not_found and all-down cases — carriers without the
        # exact GLM native pre-fail locally and the valid ones re-race in
        # parallel. Hyperfusion maps its gonka/-prefixed GLM native on the
        # entry route (nativeByProvider), so here it pre-fails locally and is
        # skipped; the gonka carriers serve the plain native.
        {
          route = "smart";
          action = "fallback";
          target = "smart.fallback";
        }
        {
          route = "smart.fallback";
          action = "filter";
          where = { error = { "in" = fallbackErrorClasses; }; };
        }
        {
          route = "smart.fallback";
          action = "filter";
          where = { provider = { "in" = allProviders; notIn = smartExcluded; unused = true; }; };
        }
        {
          route = "smart.fallback";
          action = "map";
          native = "zai-org/GLM-5.3-Flash";
        }
        { route = "smart.fallback"; action = "rank"; strategy = "priority"; }
        { route = "smart.fallback"; action = "race"; count = 0; }
      ];
  };

  # F12 observability: Grafana admin password comes from an agenix secret via
  # the file provider (never plaintext in the store). Datasources (Prometheus +
  # Loki) and dashboard provisioning are configured by the module; only the
  # secret is node-specific. Everything binds 127.0.0.1 (non-public).
  # F14: public-facing external URL — mesh-canonical. root_url drives the OIDC
  # callback (/login/generic_oauth), so it is the external https host, not the
  # loopback listener (which stays 127.0.0.1:9215 for Caddy); LAN browsers reach
  # the same server through http://grafana.<node>.local; the LAN Caddy vhost
  # keeps that browser flow on http://auth.<node>.local while mesh requests
  # continue through https://auth.homelab.myt.su.
  lattice.grafana = {
    adminPasswordFile = config.age.secrets.grafana-admin-password.path;
    secretKeyFile = config.age.secrets.grafana-secret-key.path;
    domain = "https://grafana.${meshDomain}";
  };

  # F14: центральный SSO (Authentik) за Caddy-ингрессом. Loopback-only; наружу
  # выставляется только Caddy-сайтом `auth` (см. sso-профиль и tcp-gateway).
  # Значения всех секретов — только runtime-файлами agenix (EnvironmentFile),
  # в Nix store не попадают.
  lattice.authentik = {
    secretKeyFile = config.age.secrets.authentik-secret-key.path;
    bootstrapTokenFile = config.age.secrets.authentik-bootstrap-token.path;
    bootstrapUserFile = config.age.secrets.authentik-bootstrap-user.path;
    bootstrapEmailFile = config.age.secrets.authentik-bootstrap-email.path;
    bootstrapPasswordFile = config.age.secrets.authentik-bootstrap-password.path;
    # F14: acp-ui (статический web-клиент ACP, f13-01, без собственного SSO)
    # оборачивается в Caddy ForwardAuth — защищается только браузерный UI,
    # backend-контракт ACP (forms/ws) не трогается.
    forwardAuth = [
      {
        service = "acp-ui";
      }
    ];
    oidcApplications = [
      {
        service = "grafana";
        clientId = "grafana";
        clientSecretFile = config.age.secrets.grafana-oauth-client-secret.path;
        callbackPath = "/login/generic_oauth";
      }
    ];
  };

  # F14: нативный OIDC-вход Grafana через Authentik. client_secret — agenix-секрет,
  # не в store. Backend-контракты OIDC остаются mesh-canonical; LAN Caddy
  # переписывает только browser-facing redirects на auth.<node>.local, поэтому
  # локальному клиенту для страницы входа Yggdrasil не нужен.
  # Provider/application и оба redirect URI объявлены выше в
  # lattice.authentik.oidcApplications и применяются Authentik Blueprint'ом.
  lattice.grafana.oauth = {
    clientId = "grafana";
    clientSecretFile = config.age.secrets.grafana-oauth-client-secret.path;
    authUrl = "https://auth.${meshDomain}/application/o/authorize/";
    tokenUrl = "https://auth.${meshDomain}/application/o/token/";
    apiUrl = "https://auth.${meshDomain}/application/o/userinfo/";
    scopes = [ "openid" "profile" "email" ];
    adminGroup = "authentik Admins";
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

  users.groups.authentik-oidc-secrets = { };
  users.users.authentik.extraGroups = [ "authentik-oidc-secrets" ];
  users.users.grafana.extraGroups = [ "authentik-oidc-secrets" ];
  # Keep the runtime dependency explicit in the unit as well as in the user
  # database. This makes `nixos-rebuild switch` restart Grafana when secret
  # access is introduced instead of leaving an already-running process with
  # its old supplementary-group set (which makes OAuth fail as invalid_client).
  systemd.services.grafana.serviceConfig.SupplementaryGroups = [ "authentik-oidc-secrets" ];
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
    # F14: Authentik SSO data (media/storage) survives reboots (impermanence).
    { directory = "/var/lib/authentik"; user = "authentik"; group = "authentik"; mode = "0750"; }
  ];

  system.stateVersion = "26.05";
}
