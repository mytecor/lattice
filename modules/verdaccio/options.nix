{
  lib,
  pkgs,
  ...
}:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.verdaccio = {
    enable = mkEnableOption "the Verdaccio npm caching proxy (Lattice cache plane)";

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.verdaccio;
      defaultText = lib.literalExpression "pkgs.lattice.verdaccio";
      description = "The verdaccio package to run.";
    };

    user = mkOption {
      type = types.str;
      default = "verdaccio";
      description = "System user running the registry.";
    };

    group = mkOption {
      type = types.str;
      default = "verdaccio";
      description = "System group of the registry user.";
    };

    host = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = ''
        Address to bind the HTTP server to. Keep it on loopback: clients on the
        node itself reach the registry directly, and anything beyond that is an
        operator-controlled exposure decision (Caddy ingress / tcp-gateway).
      '';
    };

    port = mkOption {
      type = types.port;
      description = "TCP port the registry listens on (loopback).";
    };

    upstreamRegistry = mkOption {
      type = types.str;
      default = "https://registry.npmjs.org";
      description = "Upstream npm registry base URL used by the proxy uplink.";
    };

    cacheRoot = mkOption {
      type = types.path;
      default = "/var/cache/verdaccio";
      description = ''
        Persistent directory holding the cached registry metadata and tarballs.
        Disposable: deleting it causes an ordinary refetch from the uplink on
        the next cold install. Because it is a cache and never a source of
        truth, it intentionally stays under /var/cache (not /var/lib).
      '';
    };

    publish = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Allow publishing/unpublishing to this registry. Defaults to false: the
        registry is a cache-only proxy (anonymous read within the closed LAN,
        publish denies everyone). When true, an htpasswd file is mounted from
        `credentials.htpasswdFile` and publish requires an authenticated user.
      '';
    };

    credentials = mkOption {
      type = types.submodule {
        options.htpasswdFile = mkOption {
          type = types.nullOr types.path;
          default = null;
          description = ''
            Runtime path (e.g. an agenix secret) to an htpasswd file with
            registry credentials. Mounted via systemd LoadCredential, never
            read from argv or the store. Only used when `publish` is enabled.
          '';
        };
      };
      default = { };
      description = "Registry credentials (used only for authenticated publish).";
    };

    maxBodySize = mkOption {
      type = types.str;
      default = "10mb";
      description = "Maximum accepted request body size (limits option, e.g. 10mb).";
    };

    logLevel = mkOption {
      type = types.enum [ "fatal" "error" "warn" "info" "debug" "trace" ];
      default = "warn";
      description = "Verdaccio log verbosity (stdout).";
    };

    runtimeDirectory = mkOption {
      type = types.str;
      default = "verdaccio";
      description = "systemd RuntimeDirectory name (private /run dir).";
    };

    # f9-03: point the node's own npm/pnpm/yarn at the loopback proxy. This is
    # the "no global manual setup" contract: package managers running on the
    # node resolve through the cached registry by default. It only writes
    # registry-URL config files (never credentials) and stays out of the
    # f8-03 base-tools contract (no toolchain is added, only a registry URL).
    clientConfig = mkOption {
      type = types.bool;
      default = true;
      description = ''
        Install global registry config (/etc/npmrc plus a yarnrc) that points
        npm, pnpm and yarn on this node at the loopback Verdaccio proxy, so
        clients use the cache without manual setup. pnpm reads npm's registry
        config, so one npmrc covers npm and pnpm; yarn reads /etc/yarnrc. Only
        a registry URL is written; no upstream credentials ever.
      '';
    };
  };
}
