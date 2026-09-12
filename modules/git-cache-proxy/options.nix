{
  lib,
  pkgs,
  ...
}:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.git-cache-proxy = {
    enable = mkEnableOption "the read-only Git cache proxy (Lattice cache plane)";

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.git-cache-proxy;
      defaultText = lib.literalExpression "pkgs.lattice.git-cache-proxy";
      description = "The git-cache-proxy package to run.";
    };

    user = mkOption {
      type = types.str;
      default = "git-cache-proxy";
      description = "System user running the proxy.";
    };

    group = mkOption {
      type = types.str;
      default = "git-cache-proxy";
      description = "System group of the proxy user.";
    };

    host = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = ''
        Address to bind the HTTP server to. Keep it on loopback: publish the
        proxy through the Caddy ingress (tcp-gateway profile) so that
        "can reach the port" stays an operator-controlled decision.
      '';
    };

    port = mkOption {
      type = types.port;
      description = "TCP port the proxy listens on (loopback).";
    };

    upstream = mkOption {
      type = types.str;
      description = "Origin git base URL, e.g. https://github.com. Repo paths are appended.";
    };

    upstreamAuthHeaderFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Runtime path (e.g. an agenix secret) containing the full HTTP header for
        upstream git clone/fetch, e.g. `Authorization: Basic <base64>`. Mounted
        into the service via systemd LoadCredential, never passed in argv.
      '';
    };

    serveTokenFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Optional runtime path to a bearer token clients must present. When null
        the proxy serves anonymously (allowed only for a network-restricted
        deployment reachable solely through the operator-controlled ingress).
      '';
    };

    allowRepos = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = ''
        Repo-scoped authorization allowlist (f9-02): the exact repository paths
        (as appended to `upstream`) this proxy may serve, e.g.
        `[ "mytecor/lattice" ]` for upstream `https://github.com`. A request
        for any repository outside the list is refused with 404 before any
        upstream fetch or cache read, even if a mirror for it already sits in
        the cache. Empty = serve any repository (insecure with an upstream
        credential).
      '';
    };

    cacheRoot = mkOption {
      type = types.path;
      default = "/var/cache/git-cache-proxy";
      description = "Directory holding bare mirror caches (disposable).";
    };

    runtimeDirectory = mkOption {
      type = types.str;
      default = "git-cache-proxy";
      description = "systemd RuntimeDirectory name (private /run dir).";
    };

    fetchTtlSeconds = mkOption {
      type = types.int;
      default = 10;
      description = "Skip upstream fetch if the mirror was refreshed within this window (0 = always fetch).";
    };

    cacheMaxMb = mkOption {
      type = types.int;
      default = 0;
      description = "Cap total on-disk mirror cache in MiB; 0 = unlimited (LRU eviction off).";
    };

    maxConcurrentRequests = mkOption {
      type = types.int;
      default = 64;
      description = "Max concurrent in-flight requests (excess queue); 0 = unlimited.";
    };

    maxDecodedBodyMb = mkOption {
      type = types.int;
      default = 512;
      description = "Cap on a decoded upload-pack request body in MiB.";
    };
  };
}
