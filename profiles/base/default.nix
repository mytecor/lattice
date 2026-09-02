{ lib, ... }:

{
  imports = [
    ../gitops/config.nix
  ];

  nix = {
    settings = {
      experimental-features = [ "nix-command" "flakes" ];
      auto-optimise-store = true;
    };

    gc = {
      automatic = true;
      dates = "weekly";
      options = "--delete-older-than 30d";
    };
  };

  nixpkgs.config.allowUnfreePredicate = package:
    builtins.elem (lib.getName package) [ "rns-server" "rnsh" ];
}
