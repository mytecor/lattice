{ config, lib, pkgs, ... }:
let
  cfg = config.lattice.embedding-runtime;
  inherit (lib) mkEnableOption mkOption types;
in {
  options.lattice.embedding-runtime = {
    enable = mkEnableOption "offline EmbeddingGemma 2 inference";
    package = mkOption {
      type = types.package;
      default = pkgs.lattice.embedding-runtime;
      description = "Runtime package including the disposable model worker.";
    };
    modelPath = mkOption {
      type = types.path;
      default = cfg.package.weights;
      description = "Pinned local model snapshot; runtime never downloads weights.";
    };
    port = mkOption { type = types.port; default = 9225; description = "Loopback HTTP listener."; };
    idleSeconds = mkOption { type = types.ints.positive; default = 1200; description = "Unload the worker after this many idle seconds."; };
    requestTimeoutSeconds = mkOption { type = types.ints.positive; default = 300; description = "Deadline including queue, loading and inference."; };
    threads = mkOption { type = types.ints.positive; default = 2; description = "CPU inference threads."; };
    memoryMax = mkOption { type = types.str; default = "6G"; description = "systemd memory limit; validate against measured peak RSS before enabling."; };
  };
  config = lib.mkIf cfg.enable {
    systemd.services.embedding-runtime = {
      description = "Offline multimodal embedding runtime";
      wantedBy = [ "multi-user.target" ];
      before = [ "llm-gateway.service" ];
      environment = {
        HF_HUB_OFFLINE = "1";
        TRANSFORMERS_OFFLINE = "1";
        TOKENIZERS_PARALLELISM = "false";
        OMP_NUM_THREADS = toString cfg.threads;
        TMPDIR = "/run/embedding-runtime";
        XDG_CACHE_HOME = "/run/embedding-runtime/cache";
        HF_HOME = "/run/embedding-runtime/huggingface";
        NUMBA_CACHE_DIR = "/run/embedding-runtime/numba";
        TORCH_HOME = "/run/embedding-runtime/torch";
      };
      serviceConfig = {
        ExecStart = lib.escapeShellArgs [
          "${cfg.package}/bin/embedding-runtime"
          "--model-path" (toString cfg.modelPath)
          "--port" (toString cfg.port)
          "--idle-seconds" (toString cfg.idleSeconds)
          "--timeout-seconds" (toString cfg.requestTimeoutSeconds)
          "--threads" (toString cfg.threads)
        ];
        DynamicUser = true;
        RuntimeDirectory = "embedding-runtime";
        RuntimeDirectoryMode = "0700";
        WorkingDirectory = "/run/embedding-runtime";
        Restart = "on-failure";
        RestartSec = 2;
        KillMode = "control-group";
        MemoryMax = cfg.memoryMax;
        Nice = 10;
        CPUQuota = "200%";
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
