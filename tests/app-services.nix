{ nixpkgs, pkgs, appServicesProfile }:

let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = [
      appServicesProfile
      {
        nixpkgs.pkgs = nixpkgs.legacyPackages.x86_64-linux;
        networking.hostName = "node-a";
        system.stateVersion = "26.05";
      }
    ];
  }).config;

  statusHost = "status.node-a.local";
  gateway = config.services.caddy.virtualHosts."http://${statusHost}";
in
assert !config.services.nginx.enable;
assert config.services.caddy.enable;
assert lib.hasInfix "respond" gateway.extraConfig;
assert lib.hasInfix "lattice-node-status" gateway.extraConfig;
assert lib.hasInfix "\"node\":\"node-a\"" gateway.extraConfig;
assert config.services.avahi.publish.userServices;
assert builtins.hasAttr "node-status-mdns" config.systemd.services;
assert lib.hasInfix statusHost config.systemd.services.node-status-mdns.script;
assert config.networking.firewall.allowedTCPPorts == [ 80 ];
pkgs.runCommand "app-services-profile-evaluation" { } "touch $out"
