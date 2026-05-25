{ config, lib, ... }:
{
  config = {
    environment.persistence."/persist" = {
      hideMounts = true;
      directories = [
        "/var/log"
        "/var/lib/nixos"
      ];
      files = [
        "/etc/machine-id"
      ];
    };

    # Точки для данных и кэша
    environment.persistence."/data".hideMounts = true;
    environment.persistence."/var/cache".hideMounts = true;
  };
}
