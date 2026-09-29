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
  # f15-02: GitHub deploy key for the node's push to mytecor/lattice (repo-scoped,
  # write, ssh-ed25519). The operator creates + encrypts it (see
  # nodes/mytecor-homelab/README.md and roadmap/f15-node-dev-loop/f15-02); the
  # secret is wired only when the .age file exists, so evaluation does not break
  # before the key is provisioned (same convention as caddy-cloudflare/jev).
  githubDeployKeyFile = ./secrets/github-lattice-deploy-key.age;
  hasGithubDeployKey = builtins.pathExists githubDeployKeyFile;
  # f10-02: r1s cluster join token (r1s1:<...>) for the r1sd allocator. Created
  # + encrypted by the operator (see nodes/mytecor-homelab/README.md); the
  # worker-runtime service is wired only when the .age file exists, so
  # evaluation does not break before the token is provisioned. The decrypted
  # secret must be readable by the r1s service user.
  r1sClusterTokenFile = ./secrets/r1s-cluster-token.age;
  hasR1sClusterToken = builtins.pathExists r1sClusterTokenFile;
  # f15-02: Radicle peer profile of the node, strictly separate from the seed
  # profile (RAD_HOME=/var/lib/radicle, rad-system). The peer identity lives on
  # the node itself (generated in place via `rad-peer auth`), survives reboot
  # through impermanence, and is what `git push rad://...` signs with.
  # Peer DID of the node (public, generated on the node 2026-09-23):
  #   did:key:z6MkqUjzpiYfDAcjnj2379bYfEk4DdLtWQkyfk7nECn6HyZx
  radiclePeerHome = "/persist/var/lib/radicle-peer";
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
  # Google AI Studio joins the `smart` logical model as soon as its existing
  # API key is encrypted at this path. Vertex is provisioned independently and
  # remains the active Gemini carrier until then.
  googleAiStudioKeyFile = ./secrets/llm-provider-google-ai-studio.age;
  hasGoogleAiStudioKey = builtins.pathExists googleAiStudioKeyFile;
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
  # закрыт Authentik SSO (F14, нативный OIDC). llm-gateway (с 2026-09-28) тоже открыт
  # на mesh (https://llm-gateway.homelab.myt.su): client-auth настраивается через
  # settings.client_api_keys (node-pi, mac) и включается при следующей пересборке ноды; до этого
  # доступ как раньше — только для доверенных участников yggdrasil-сети (mesh — не
  # публичный интернет). meshExclude не задан — ни один сервис не исключён из mesh;
  # если сервису нужно остаться только на LAN-контракте *.local, добавить его сюда.
  lattice.tcp-gateway = {
    meshDomain = "homelab.myt.su";
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
      llm-provider-google-vertex = {
        file = ./secrets/llm-provider-google-vertex-credentials.age;
        mode = "0400";
      };
      # LLM Gateway client keys (settings.client_api_keys): по одному на потребителя.
      # node-pi — Pi на самой ноде (loopback); mac — операторский Mac (mDNS
      # llm-gateway + mesh llm-gateway-mesh). Значения созданы оператором и
      # зашифрованы для [admin node]. При смене значения gateway перезапускается
      # пересборкой (изменяются unit) либо вручную: systemctl restart llm-gateway
      # (env unit пересоберётся через PartOf).
      llm-gateway-client-node-pi = {
        file = ./secrets/llm-gateway-client-node-pi.age;
        mode = "0400";
      };
      llm-gateway-client-mac = {
        file = ./secrets/llm-gateway-client-mac.age;
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
    } // lib.optionalAttrs hasGithubDeployKey {
      # f15-02: GitHub deploy key (repo-scoped write for mytecor/lattice) for the
      # workspace `publish` push from the node. Registered only when the operator
      # created the .age file (see nodes/mytecor-homelab/README.md and
      # roadmap/f15-node-dev-loop/f15-02); mode 0400, root-only. The private key
      # never enters the Nix store (agenix decrypts at activation) and root's ssh
      # alias (profiles/node-dev) points IdentityFile at this path.
      github-lattice-deploy-key = {
        file = githubDeployKeyFile;
        mode = "0400";
      };
    } // lib.optionalAttrs hasR1sClusterToken {
      # f10-02: r1s cluster join token for the r1sd allocator. Decrypted agenix
      # secret is chowned to the r1s service user so `worker-runtime` preStart can
      # read it at runtime (never the store/argv/journal). The module's own
      # assertion requires it when enabled.
      r1s-cluster-token = {
        file = r1sClusterTokenFile;
        mode = "0400";
        owner = "r1s";
        group = "r1s";
      };
    } // lib.optionalAttrs hasGoogleAiStudioKey {
      llm-provider-google-ai-studio = {
        file = googleAiStudioKeyFile;
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
      # Реальный client-ключ node-pi подаётся runtime-ссылкой (Pi value
      # resolution `!cmd`): значение читается из agenix-секрета при старте Pi,
      # в Nix store не попадает. Файл .age создан без завершающего перевода
      # строки, поэтому Bearer совпадает с gateway точно.
      apiKey = "!cat /run/agenix/llm-gateway-client-node-pi";
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
        # smart → gemini-3.8-flash through the independent Vertex and AI Studio
        # quota pools. Bifrost maps OpenAI reasoning_effort to Gemini's native
        # thinkingLevel; the current Gemini 3 fallback ladder is low/medium/high.
        smart = {
          reasoning = true;
          input = [ "text" "image" ];
          thinkingLevelMap = {
            off = null;
            minimal = "low";
            low = "low";
            medium = "medium";
            high = "high";
            xhigh = "high";
            max = "high";
          };
          compat = { supportsReasoningEffort = true; };
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
    # same bash/git/tools contract as the local runtime. f15-02: add rad-peer
    # (rad against the node's own peer profile) so an agent can inspect its
    # peer identity and push from the session.
    path = [ pkgs.lattice.pi-tool-profile pkgs.lattice.rad-peer ];
    # f15-02: the Git-side radicle remote helper for rad:// push reads RAD_HOME
    # from the sessions' environment; point it at the node's persistent peer
    # profile so `git push publish main` signs with the peer identity, never the
    # seed profile.
    extraEnv = {
      RAD_HOME = radiclePeerHome;
      LATTICE_RADICLE_PEER_HOME = radiclePeerHome;
    };
    # f15-01: ACP sessions open in the node's Lattice working checkout (see
    # profiles/node-dev), so an agent can edit, commit and push `main` from
    # the node itself.
    defaultCwd = "/var/lib/lattice-workspace/lattice";
    # ACP has no pagination: `session/load` / REST history serve only the last
    # sessionHistoryMaxEntries entries. Long sessions (pi streams one
    # `agent_message_chunk` per token/reply) easily exceed the daemon default
    # (10000) and then silently lose every older user/assistant message from
    # acp-ui. Raise the limit so the whole recorded history stays loadable
    # instead of being truncated (see modules/pi-acp-daemon options).
    sessionHistoryMaxEntries = 200000;
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

  # f10-02: r1sd allocator (F10 disposable-worker execution backend). Enabled
  # only once the operator created the cluster join token (r1s-cluster-token.age).
  # RNS control plane: F22 r1sd attaches as a client to the node's shared RNS
  # instance (rns-server, share_instance = Yes; no private Reticulum stack, no
  # --rns-config), OCI via the local containerd socket (group r1s, not exposed).
  # See modules/worker-runtime/README.md.
  lattice.worker-runtime = lib.mkIf hasR1sClusterToken {
    enable = true;
    clusterTokenFile = config.age.secrets.r1s-cluster-token.path;
  };

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

  # LLM Gateway: the `settings` attribute is the gateway's public JSON contract
  # verbatim. Field names are snake_case and every routing rule is written
  # literally; the NixOS module does not generate pipelines or translate a
  # second Nix-specific schema. Secrets remain separate runtime paths in
  # `credentials` and are referenced from settings as env.NAME.
  lattice.llm-gateway = {
    credentials = {
      LATTICE_CLIENT_NODE_PI_KEY = config.age.secrets.llm-gateway-client-node-pi.path;
      LATTICE_CLIENT_MAC_KEY = config.age.secrets.llm-gateway-client-mac.path;
      LATTICE_LLM_PROVIDER_GONKA_PROXY_KEY = config.age.secrets.llm-provider-gonka-gg-proxy.path;
      LATTICE_LLM_PROVIDER_GONKA_OPENBROKER_KEY = config.age.secrets.llm-provider-gonka-gg-openbroker.path;
      LATTICE_LLM_PROVIDER_GONKA_API_KEY = config.age.secrets.llm-provider-gonka-api.path;
      LATTICE_LLM_PROVIDER_DAHL_KEY = config.age.secrets.llm-provider-dahl.path;
      LATTICE_LLM_PROVIDER_DAHL_2_KEY = config.age.secrets.llm-provider-dahl-2.path;
      LATTICE_LLM_PROVIDER_HYPERFUSION_KEY = config.age.secrets.llm-provider-hyperfusion.path;
      LATTICE_LLM_PROVIDER_GONKAROUTER_KEY = config.age.secrets.llm-provider-gonkarouter.path;
      LATTICE_LLM_PROVIDER_GOOGLE_VERTEX_CREDENTIALS = config.age.secrets.llm-provider-google-vertex.path;
    } // lib.optionalAttrs hasGoogleAiStudioKey {
      LATTICE_LLM_PROVIDER_GOOGLE_AI_STUDIO_KEY = config.age.secrets.llm-provider-google-ai-studio.path;
    };

    settings = {
      log_level = "debug";
      stream_idle_timeout = "5m";
      client_api_keys = [
        { id = "node-pi"; key = "env.LATTICE_CLIENT_NODE_PI_KEY"; }
        { id = "mac"; key = "env.LATTICE_CLIENT_MAC_KEY"; }
      ];
      providers = [
        {
          id = "gonka-proxy";
          base_provider = "openai";
          inference_url = "https://api.proxy.gonka.gg/v1";
          api_key = "env.LATTICE_LLM_PROVIDER_GONKA_PROXY_KEY";
          priority = 50;
          strip_params = [ "thinking" "reasoning_effort" ];
          set_params.thinking.type = "disabled";
        }
        {
          id = "gonka-openbroker";
          base_provider = "openai";
          inference_url = "https://api.openbroker.gonka.gg/v1";
          api_key = "env.LATTICE_LLM_PROVIDER_GONKA_OPENBROKER_KEY";
          priority = 40;
          strip_params = [ "thinking" "reasoning_effort" ];
          set_params.thinking.type = "disabled";
        }
        {
          id = "gonka-api";
          base_provider = "openai";
          inference_url = "https://hskyauefqcgbvgvxkluj.supabase.co/functions/v1/gonka";
          api_key = "env.LATTICE_LLM_PROVIDER_GONKA_API_KEY";
          priority = 30;
          strip_params = [ "thinking" "reasoning_effort" ];
          set_params.thinking.type = "disabled";
        }
        { id = "dahl"; base_provider = "openai"; inference_url = "https://inference.dahl.global/v1"; api_key = "env.LATTICE_LLM_PROVIDER_DAHL_KEY"; priority = 20; strip_params = [ "thinking" "reasoning_effort" ]; }
        { id = "dahl-2"; base_provider = "openai"; inference_url = "https://inference.dahl.global/v1"; api_key = "env.LATTICE_LLM_PROVIDER_DAHL_2_KEY"; priority = 20; strip_params = [ "thinking" "reasoning_effort" ]; }
        { id = "hyperfusion"; base_provider = "openai"; inference_url = "https://api.hyperfusion.io/v1"; api_key = "env.LATTICE_LLM_PROVIDER_HYPERFUSION_KEY"; priority = 100; strip_params = [ "thinking" "reasoning_effort" ]; }
        { id = "gonkarouter"; base_provider = "openai"; inference_url = "https://api.gonkarouter.io/v1"; api_key = "env.LATTICE_LLM_PROVIDER_GONKAROUTER_KEY"; priority = 10; strip_params = [ "thinking" "reasoning_effort" ]; }
        {
          id = "google-vertex";
          base_provider = "vertex";
          inference_url = "https://aiplatform.googleapis.com";
          vertex_auth_credentials = "env.LATTICE_LLM_PROVIDER_GOOGLE_VERTEX_CREDENTIALS";
          vertex_project_id = "mytecor";
          vertex_region = "global";
          priority = 100;
        }
      ] ++ lib.optionals hasGoogleAiStudioKey [{
        id = "google-ai-studio";
        base_provider = "gemini";
        inference_url = "https://generativelanguage.googleapis.com/v1beta";
        api_key = "env.LATTICE_LLM_PROVIDER_GOOGLE_AI_STUDIO_KEY";
        priority = 90;
      }];

      routing_rules = [
        # stupid
        { route = "stupid"; action = "filter"; where.model.eq = "stupid"; }
        {
          route = "stupid";
          action = "map";
          providers = [ "gonka-proxy" "gonka-openbroker" "gonka-api" "dahl" "dahl-2" "hyperfusion" "gonkarouter" ];
          native = "MiniMaxAI/MiniMax-M2.7";
          tier = 0;
        }
        {
          route = "stupid";
          action = "admission";
          maxInFlight = 4;
          maxPending = 16;
          waitTimeout = "30s";
        }
        { route = "stupid"; action = "rank"; strategy = "priority"; }
        { route = "stupid"; action = "balance"; strategy = "expected-ttft"; window = "5m"; error_budget = 0.2; }
        { route = "stupid"; action = "affinity"; sources = [ "responses.conversation" "responses.previous_response_id" ]; ttl = "24h"; on_missing = "ignore"; on_provider_failure = "fail-closed"; }
        { route = "stupid"; action = "race"; count = 1; }
        { route = "stupid"; action = "retry"; attempts = 2; backoff = { type = "exponential"; initial = "200ms"; max = "1s"; }; }
        { route = "stupid"; action = "hedge"; after = "20s"; }
        { route = "stupid"; action = "timeout"; duration = "60s"; }

        # standard
        { route = "standard"; action = "filter"; where.model.eq = "standard"; }
        {
          route = "standard";
          action = "map";
          providers = [ "gonka-proxy" "gonka-openbroker" "gonka-api" "dahl" "dahl-2" "hyperfusion" "gonkarouter" ];
          native = "deepseek-ai/DeepSeek-V4-Flash-0731";
          tier = 0;
        }
        {
          route = "standard";
          action = "map";
          providers = [ "hyperfusion" ];
          native = "gonka/deepseek-ai/DeepSeek-V4-Flash-0731";
          tier = 1;
        }
        {
          route = "standard";
          action = "admission";
          maxInFlight = 4;
          maxPending = 16;
          waitTimeout = "30s";
        }
        { route = "standard"; action = "rank"; strategy = "priority"; }
        { route = "standard"; action = "balance"; strategy = "expected-ttft"; window = "5m"; error_budget = 0.2; }
        { route = "standard"; action = "affinity"; sources = [ "responses.conversation" "responses.previous_response_id" ]; ttl = "24h"; on_missing = "ignore"; on_provider_failure = "fail-closed"; }
        { route = "standard"; action = "race"; count = 1; }
        { route = "standard"; action = "retry"; attempts = 2; backoff = { type = "exponential"; initial = "200ms"; max = "1s"; }; }
        { route = "standard"; action = "hedge"; after = "20s"; }
        { route = "standard"; action = "timeout"; duration = "60s"; }

        # smart
        { route = "smart"; action = "filter"; where.model.eq = "smart"; }
        {
          route = "smart";
          action = "map";
          providers = [ "google-vertex" ] ++ lib.optional hasGoogleAiStudioKey "google-ai-studio";
          native = "gemini-3.8-flash";
          tier = 0;
        }
        {
          route = "smart";
          action = "admission";
          maxInFlight = 6;
          maxPending = 16;
          waitTimeout = "30s";
        }
        { route = "smart"; action = "rank"; strategy = "priority"; }
        { route = "smart"; action = "balance"; strategy = "round_robin"; weights = { }; window = "5m"; error_budget = 0.2; }
        { route = "smart"; action = "affinity"; sources = [ "responses.conversation" "responses.previous_response_id" ]; ttl = "24h"; on_missing = "ignore"; on_provider_failure = "fail-closed"; }
        { route = "smart"; action = "race"; count = 1; }
        { route = "smart"; action = "retry"; attempts = 2; backoff = { type = "exponential"; initial = "200ms"; max = "1s"; }; }
        { route = "smart"; action = "timeout"; duration = "60s"; }
      ];
    };
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
    # f10-02: r1sd allocator identity/state survive reboots (impermanence).
    # No user/group here: the r1s user is brand-new and does not exist in the
    # running generation during activation, so a persist entry that chowns to
    # it (user = "r1s") aborts the switch ('createPersistentStorageDirs'
    # chown fails on a not-yet-created user). Ownership is established by the
    # worker-runtime service's StateDirectory at first start instead.
    { directory = "/var/lib/worker-runtime"; mode = "0700"; }
    # f15-02: Radicle peer profile of the node (rad-peer / RAD_HOME) survives
    # reboots. The seed profile (/var/lib/radicle) has its own entry above.
    { directory = radiclePeerHome; user = "root"; group = "root"; mode = "0700"; }
    # F12 observability data survives reboots (impermanence).
    { directory = "/var/lib/prometheus"; user = "prometheus"; group = "prometheus"; mode = "0750"; }
    { directory = "/var/lib/loki"; user = "loki"; group = "loki"; mode = "0750"; }
    { directory = "/var/lib/grafana"; user = "grafana"; group = "grafana"; mode = "0750"; }
    # F14: Authentik SSO data (media/storage) survives reboots (impermanence).
    { directory = "/var/lib/authentik"; user = "authentik"; group = "authentik"; mode = "0750"; }
  ];

  system.stateVersion = "26.05";
}
