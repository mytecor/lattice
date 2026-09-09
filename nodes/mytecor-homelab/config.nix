{ config, lib, pkgs, ... }:

let
  rootPasswordHashFile = ./secrets/root-password-hash.age;
  hasRootPassword = builtins.pathExists rootPasswordHashFile;
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
    };
  };

  lattice.wireless.networks = [
    {
      ssid = config.age.secrets.wifi-ssid.path;
      password = config.age.secrets.wifi-password.path;
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
    };
    models.llm-gateway = {
      baseUrl = "http://127.0.0.1:9208/v1";
      api = "openai-completions";
      # f8-02: без client auth. Если включим clientKey позже — apiKey задаётся env-ссылкой,
      # не литералом в store.
      discoverModels = false;
      models = [
        { id = "standard"; }
        { id = "stupid"; }
      ];
      modelOverrides = {
        standard = {
          thinkingLevelMap = {
            off = null; minimal = null; low = null; medium = null;
            high = null; xhigh = null; max = null;
          };
          compat = { supportsReasoningEffort = false; };
        };
        stupid = {
          thinkingLevelMap = {
            off = null; minimal = null; low = null; medium = null;
            high = null; xhigh = null; max = null;
          };
          compat = { supportsReasoningEffort = false; };
        };
      };
    };
  };

  # LLM Gateway: bounded routing splits the flat pipeline into single-purpose
  # actions. Each stage starts from explicit map actions that bind one native
  # model id to a set of provider IDs; the candidate pool is ranked by provider
  # priority, a winner lease promotes the current leader, Responses affinity
  # pins stateful chains to their originating provider, and race/retry/hedge
  # consume only the next unused targets within the semaphore budget. One
  # request never creates more than four upstream calls (race 2 + two
  # next-retries of 1), more than three concurrent calls, or a repeated call to
  # one provider. Provider transport and credentials stay in the registry;
  # routing never references access groups.
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
    # The primary stage maps one native per logical model to the full provider
    # set; the fallback stage for `standard` gives Hyperfusion its second
    # catalog alias so a model_not_found in the primary alias can fail over to
    # the prefixed native Hyperfusion actually serves.
    routingRules = let
      allProviders = [
        "gonka-proxy"
        "gonka-openbroker"
        "gonka-api"
        "dahl"
        "hyperfusion"
        "gonkarouter"
      ];
      # The bounded primary pipeline used by both logical models.
      primaryRules = model: native: [
        { inherit model; action = "map"; native = native; providers = allProviders; }
        { inherit model; action = "rank"; strategy = "priority"; }
        {
          inherit model;
          action = "lease";
          source = "winner";
          duration = "10m";
          renewOnSuccess = true;
          releaseOn = [ "429" "5xx" "timeout" "connection_error" ];
          releaseAfterSlowStarts = 3;
          slowStart = "3s";
        }
        {
          inherit model;
          action = "affinity";
          sources = [ "responses.conversation" "responses.previous_response_id" ];
          ttl = "24h";
          onMissing = "ignore";
          onProviderFailure = "fail-closed";
        }
        { inherit model; action = "race"; count = 2; }
        {
          inherit model;
          action = "retry";
          scope = "next";
          count = 1;
          attempts = 2;
          on = [ "429" "5xx" "timeout" "connection_error" "invalid_response" ];
          backoffInitial = "200ms";
          backoffMax = "1s";
        }
        { inherit model; action = "hedge"; after = "3s"; }
        {
          inherit model;
          action = "semaphore";
          maxCalls = 4;
          maxInFlight = 3;
          maxCallsPerProvider = 1;
        }
        { inherit model; action = "timeout"; duration = "60s"; }
      ];
      # Fallback stage for standard: Hyperfusion accepts both catalog aliases.
      standardFallback = [
        {
          model = "standard";
          action = "map";
          native = "gonka/deepseek-ai/DeepSeek-V4-Flash-0731";
          providers = [ "hyperfusion" ];
        }
        {
          model = "standard";
          action = "fallback";
          fallbackStrategy = "race";
          on = [ "model_not_found" "429" "5xx" "timeout" "connection_error" ];
        }
      ];
    in
    primaryRules "stupid" "MiniMaxAI/MiniMax-M2.7"
    ++ primaryRules "standard" "deepseek-ai/DeepSeek-V4-Flash-0731"
    ++ standardFallback;
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
  ];

  system.stateVersion = "26.05";
}
