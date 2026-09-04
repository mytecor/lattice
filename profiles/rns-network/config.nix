{ config, lib, ... }:

let
  inherit (lib) mkOption types;
  network = import ../networking/reticulum.nix;
  ports = import ../networking/ports.nix;
  cfg = config.lattice.rns-network;
  active = lib.filterAttrs (_: peer: peer.enable) cfg.uplinks;
  addresses = map (peer: "${peer.host}:${toString peer.port}") (builtins.attrValues active);
in
{
  options.lattice.rns-network.uplinks = mkOption {
    type = types.attrsOf (types.submodule {
      options = {
        enable = mkOption {
          type = types.bool;
          default = true;
          description = "Connect to this public or private Reticulum peer.";
        };
        host = mkOption {
          type = types.addCheck types.str (host: host != "" && builtins.all
            (char: lib.hasInfix char "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-:[]%_")
            (lib.stringToCharacters host));
          description = "DNS name or IP address; bracket IPv6 literals.";
        };
        port = mkOption {
          type = types.ints.between 1 65535;
          default = ports.rns-tcp;
          description = "Remote TCP port.";
        };
      };
    });
    default = network.uplinks;
    description = "Named outbound peers, shared through profiles/networking/reticulum.nix.";
  };

  config = {
    assertions = [
      {
        assertion = active != { };
        message = "lattice.rns-network: configure at least one enabled uplink.";
      }
      {
        assertion = builtins.all (name: builtins.match "[a-zA-Z0-9_-]+" name != null)
          (builtins.attrNames cfg.uplinks);
        message = "lattice.rns-network: uplink names must contain only letters, digits, underscores or hyphens.";
      }
      {
        assertion = builtins.length addresses == builtins.length (lib.unique addresses);
        message = "lattice.rns-network: enabled uplinks must have distinct host:port addresses.";
      }
    ];
    lattice.rns-server = {
      enable = lib.mkDefault true;
      reticulum.enable_transport = lib.mkDefault false;
      server.http.enabled = lib.mkDefault false;
      interfaces = lib.mapAttrs' (name: peer: lib.nameValuePair "Uplink ${name}" {
        type = "TCPClientInterface";
        enabled = peer.enable;
        target_host = peer.host;
        target_port = peer.port;
      }) cfg.uplinks;
    };
  };
}
