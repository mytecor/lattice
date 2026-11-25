{ config, lib, pkgs, ... }:
let
  cfg = config.lattice.cpu-max;
  inherit (lib) mkEnableOption mkOption types;
in {
  options.lattice.cpu-max = {
    enable = mkEnableOption "local CPU-Max EmbeddingGemma 2 inference";
    package = mkOption { type = types.package; default = pkgs.lattice.cpu-max; };
    port = mkOption {
      type = types.port;
      default = (import ../../profiles/networking/ports.nix).cpu-max;
      description = "Loopback HTTP port.";
    };
    threads = mkOption { type = types.ints.positive; default = 4; };
    memoryMax = mkOption { type = types.str; default = "6G"; };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.cpu-max = {
      description = "CPU-Max EmbeddingGemma 2";
      wantedBy = [ "multi-user.target" ];
      before = [ "llm-gateway.service" ];
      serviceConfig = {
        ExecStart = lib.escapeShellArgs [
          (lib.getExe cfg.package)
          "--port" (toString cfg.port)
          "--threads" (toString cfg.threads)
          "--workers" "4"
          "--max-queue" "16"
          "--max-client-batch-size" "64"
          "--max-batch-sequence-tokens" "8192"
          "--tokenizer-workers" (toString cfg.threads)
        ];
        DynamicUser = true;
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
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" ];
        IPAddressDeny = "any";
        IPAddressAllow = "localhost";
        UMask = "0077";
      };
    };
  };
}
