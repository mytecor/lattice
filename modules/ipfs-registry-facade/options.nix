{ lib, pkgs, ... }:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.ipfs-registry-facade = {
    enable = mkEnableOption "IPFS-backed OCI registry facade (Kubo + nerdctl registry serve)";

    listenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = "Loopback address where the OCI registry facade listens.";
    };

    port = mkOption {
      type = types.port;
      default = 5050;
      description = "Port where the OCI registry facade listens.";
    };

    ipfsApiAddress = mkOption {
      type = types.str;
      default = "/ip4/127.0.0.1/tcp/5001";
      description = "Multiaddr of the local IPFS API (Kubo daemon).";
    };

    dataDir = mkOption {
      type = types.path;
      default = "/var/lib/ipfs-daemon";
      description = "Directory where Kubo daemon stores IPFS blocks, pins and repo state.";
    };

    kuboPackage = mkOption {
      type = types.package;
      default = pkgs.kubo;
      defaultText = lib.literalExpression "pkgs.kubo";
      description = "Pinned Kubo (IPFS) package.";
    };

    nerdctlPackage = mkOption {
      type = types.package;
      default = pkgs.nerdctl;
      defaultText = lib.literalExpression "pkgs.nerdctl";
      description = "Pinned nerdctl package providing `nerdctl ipfs registry serve`.";
    };
  };
}
