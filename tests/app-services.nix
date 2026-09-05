{ nixpkgs, pkgs, appServicesProfile }:

let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = [
      appServicesProfile
      {
        nixpkgs.pkgs = nixpkgs.legacyPackages.x86_64-linux;
        networking.hostName = "node-a";
        networking.domain = "lattice";
        system.stateVersion = "26.05";
      }
    ];
  }).config;

  statusHost = "status.node-a.lattice";
  gateway = config.services.caddy.virtualHosts."http://${statusHost}";
in
assert !config.services.nginx.enable;
assert config.services.caddy.enable;
assert lib.hasInfix "respond" gateway.extraConfig;
assert lib.hasInfix "lattice-node-status" gateway.extraConfig;
assert lib.hasInfix "\"node\":\"node-a\"" gateway.extraConfig;
assert config.networking.firewall.allowedTCPPorts == [ 80 443 ];
pkgs.runCommand "app-services-profile-evaluation" { } "touch $out"
