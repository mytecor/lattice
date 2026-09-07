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

  # LLM Gateway: Lattice-owned Go proxy races both Gonka inference endpoints.
  # Proxy owns discovery for the shared group; OpenBroker has no /v1/models.
  lattice.llm-gateway = {
    runtime = "bifrost";
    package = pkgs.lattice.llm-gateway;
    logicalModels = [ "stupid" "standard" ];
    providers = {
      proxy = {
        id = "gonka-proxy";
        accessGroup = "gonka";
        inferenceUrl = "https://proxy.gonka.gg";
        apiKeyFile = config.age.secrets.llm-provider-gonka-gg-proxy.path;
        priority = 10;
      };
      openbroker = {
        id = "gonka-openbroker";
        accessGroup = "gonka";
        inferenceUrl = "https://openbroker.gonka.gg";
        modelsUrl = "https://proxy.gonka.gg/v1/models";
        apiKeyFile = config.age.secrets.llm-provider-gonka-gg-openbroker.path;
        modelsApiKeyFile = config.age.secrets.llm-provider-gonka-gg-proxy.path;
        priority = 10;
      };
    };
    models = [
      { logical = "stupid"; accessGroup = "gonka"; native = "MiniMaxAI/MiniMax-M2.7"; }
      { logical = "standard"; accessGroup = "gonka"; native = "deepseek-ai/DeepSeek-V4-Flash-0731"; }
    ];
    routingRules = [
      { model = "stupid"; action = "race"; providers = [ "gonka-proxy" "gonka-openbroker" ]; }
      { model = "stupid"; action = "retry"; attempts = 10; on = [ "429" "5xx" "timeout" "connection_error" ]; }
      { model = "standard"; action = "race"; providers = [ "gonka-proxy" "gonka-openbroker" ]; }
      { model = "standard"; action = "retry"; attempts = 10; on = [ "429" "5xx" "timeout" "connection_error" ]; }
    ];
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
