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

          band = mkOption {
            type = types.nullOr (types.enum [ "bg" "a" ]);
            default = null;
            description = "Force the managed interface onto a band (NetworkManager: 2.4 GHz `bg` or 5 GHz `a`). Null lets NetworkManager pick. Set `bg` to keep the STA link on 2.4 GHz so the concurrent hotspot (which must share the single radio's channel) can also use 2.4 GHz.";
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
