{ lib, pkgs, ... }:

let
  inherit (lib) mkOption types;
in
{
  imports = [
    ./options-server.nix
    ./options-rnsd.nix
  ];

  options.lattice.rns-server = {
    enable = mkOption {
      type = types.bool;
      default = false;
      description = "Enable the rns-server system service.";
    };

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.rns-server;
      defaultText = lib.literalExpression "pkgs.lattice.rns-server";
      description = "Package providing the rns-server binary.";
    };

    user = mkOption {
      type = types.str;
      default = "rns";
      description = "User that runs rns-server.";
    };

    group = mkOption {
      type = types.str;
      default = "rns";
      description = "Group that runs rns-server.";
    };

    configDir = mkOption {
      type = types.str;
      default = "/var/lib/rns";
      description = "Runtime configuration directory passed to rns-server.";
    };

    extraArgs = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = "Extra arguments appended to rns-server start.";
    };
  };
}
