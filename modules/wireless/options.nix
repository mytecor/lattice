{ lib, ... }:
let inherit (lib) types literalExpression mkOption;
in
{
  options.lattice.wireless = {
    networks = mkOption {
      type = types.listOf (types.submodule {
        options = {
          ssid = mkOption {
            type = types.str;
            example = literalExpression "config.age.secrets.wifi_ssid.path";
            description = "Path to a file containing the Wi-Fi SSID.";
          };

          password = mkOption {
            type = types.str;
            example = literalExpression "config.age.secrets.wifi_psk.path";
            description = "Path to a file containing the Wi-Fi password.";
          };

          priority = mkOption {
            type = types.nullOr types.int;
            default = null;
            example = 60;
            description = ''
              NetworkManager `connection.autoconnect-priority`: a higher value wins
              when the radio auto-connects among the configured networks. Use it to
              rank networks by signal strength (stronger AP first). `null` leaves the
              priority unset and NetworkManager's defaults apply.
            '';
          };
        };
      });
      default = [ ];
      example = literalExpression ''
        [
          {
            ssid = config.age.secrets.wifi_ssid.path;
            password = config.age.secrets.wifi_psk.path;
          }
        ]
      '';
      description = "Wi-Fi networks to configure from runtime secret files.";
    };
  };
}
