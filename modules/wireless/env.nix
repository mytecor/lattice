{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.wireless;
  vars = import ./vars.nix { inherit lib; networks = cfg.networks; };
in
{
  config = {
    systemd.services.lattice-wireless-env = lib.mkIf (cfg.networks != [ ]) {
      description = "Generate NetworkManager wireless environment file";
      requiredBy = [ "NetworkManager-ensure-profiles.service" ];
      before = [ "NetworkManager-ensure-profiles.service" ];
      path = [ pkgs.coreutils ];
      script = ''
        set -eu

        require_file() {
          file="$1"
          [ -r "$file" ] || {
            printf 'Required wireless secret file is not readable: %s\n' "$file" >&2
            return 1
          }
        }

      '' + lib.concatMapStringsSep "\n" (file: ''
        require_file ${lib.escapeShellArg file}
      '') vars.secretFiles + ''

        tmp=$(mktemp ${vars.envFile}.XXXXXX)
        chmod 600 "$tmp"

        write_env() {
          key="$1"
          file="$2"
          value=$(cat "$file")
          printf '%s=%q\n' "$key" "$value" >> "$tmp"
        }

      '' + lib.concatMapStringsSep "\n" (network: ''
        write_env ${lib.escapeShellArg network.ssidVar} ${lib.escapeShellArg network.net.ssid}
        write_env ${lib.escapeShellArg network.passwordVar} ${lib.escapeShellArg network.net.password}
      '') vars.indexedNetworks + ''

        mv "$tmp" ${vars.envFile}
        chmod 600 ${vars.envFile}
      '';
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        UMask = "0177";
      };
    };

    systemd.paths.lattice-wireless-env = lib.mkIf (cfg.networks != [ ]) {
      description = "Watch NetworkManager wireless secret files";
      wantedBy = [ "multi-user.target" ];
      pathConfig = {
        PathChanged = vars.secretFiles;
        PathExists = vars.secretFiles;
        Unit = "NetworkManager-ensure-profiles.service";
      };
    };
  };
}
