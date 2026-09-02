{ config, ... }:

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
    };
  };

  lattice.wireless.networks = [
    {
      ssid = config.age.secrets.wifi-ssid.path;
      password = config.age.secrets.wifi-password.path;
    }
  ];

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

  users.users.root = {
    hashedPassword = "*";
    openssh.authorizedKeys.keys = [
      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP6Gm4DbPs1Ar7/g9IU90YS873SoMYMQhc0xjQFHtJEk mytecor@macbook.local"
    ];
  };

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
  ];

  system.stateVersion = "26.05";
}
