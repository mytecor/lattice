{ config, lib, pkgs, ... }:
let
  cfg = config.lattice.llama-embeddings;
  inherit (lib) mkEnableOption mkOption types;
in {
  options.lattice.llama-embeddings = {
    enable = mkEnableOption "native llama.cpp multimodal embeddings";
    package = mkOption { type = types.package; default = pkgs.lattice.llama-embeddings; };
    port = mkOption {
      type = types.port;
      default = (import ../../profiles/networking/ports.nix).llama-embeddings;
      description = "Loopback port for the native llama-server router.";
    };
    idleSeconds = mkOption { type = types.ints.positive; default = 1200; };
    threads = mkOption { type = types.ints.positive; default = 2; };
    memoryMax = mkOption { type = types.str; default = "6G"; };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.llama-embeddings = {
      description = "llama.cpp EmbeddingGemma 2 router";
      wantedBy = [ "multi-user.target" ];
      before = [ "llm-gateway.service" ];
      environment.LLAMA_CACHE = "/run/llama-embeddings/cache";
      serviceConfig = {
        ExecStart = lib.escapeShellArgs [
          "${cfg.package}/bin/llama-embeddings"
          "--port" (toString cfg.port)
          "--sleep-idle-seconds" (toString cfg.idleSeconds)
          "--threads" (toString cfg.threads)
          "--threads-batch" (toString cfg.threads)
        ];
        DynamicUser = true;
        RuntimeDirectory = "llama-embeddings";
        RuntimeDirectoryMode = "0700";
        Restart = "on-failure";
        RestartSec = 2;
        MemoryMax = cfg.memoryMax;
        CPUQuota = "${toString (cfg.threads * 100)}%";
        NoNewPrivileges = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        IPAddressDeny = "any";
        IPAddressAllow = "localhost";
        UMask = "0077";
      };
    };
  };
}
