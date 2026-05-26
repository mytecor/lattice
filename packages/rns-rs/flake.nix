{
  description = "rns-rs package";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfreePredicate = pkg:
            builtins.elem (nixpkgs.lib.getName pkg) [ "rns-server" "rnsh" ];
        };
      in
      {
        packages = rec {
          rns-server = pkgs.callPackage ./package.nix { bin = "rns-server"; };
          rnsh = pkgs.callPackage ./package.nix { bin = "rnsh"; };
          default = rns-server;
        };
      }
    );
}
