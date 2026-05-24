{ lib, ... }:

{
  options.lattice.wireless = {
    networks = lib.mkOption {
      type = lib.types.listOf (lib.types.submodule {
        options = {
          ssid = lib.mkOption {
            type = lib.types.str;
            example = lib.literalExpression "config.age.secrets.wifi_ssid.path";
            description = "Path to a file containing the Wi-Fi SSID.";
          };

          password = lib.mkOption {
            type = lib.types.str;
            example = lib.literalExpression "config.age.secrets.wifi_psk.path";
            description = "Path to a file containing the Wi-Fi password.";
          };
        };
      });
      default = [ ];
      example = lib.literalExpression ''
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
