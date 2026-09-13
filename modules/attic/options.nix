{
  lib,
  pkgs,
  ...
}:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.attic = {
    enable = mkEnableOption "the Attic Nix binary cache / artifact cache (Lattice cache plane)";

    package = mkOption {
      type = types.package;
      # attic-server is linux-only Rust; on macOS it can be evaluated but not
      # built or run. CI (ubuntu x86_64-linux) builds it.
      default = pkgs.attic-server;
      defaultText = lib.literalExpression "pkgs.attic-server";
      description = "The attic-server (atticd) daemon package to run.";
    };

    clientPackage = mkOption {
      type = types.package;
      default = pkgs.attic-client;
      defaultText = lib.literalExpression "pkgs.attic-client";
      description = "The attic client CLI package, used for cache administration and uploads.";
    };

    user = mkOption {
      type = types.str;
      default = "attic";
      description = "System user running the atticd service.";
    };

    group = mkOption {
      type = types.str;
      default = "attic";
      description = "System group of the attic user.";
    };

    host = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = ''
        Address to bind the atticd HTTP server to. Keep it on loopback: the
        node's own nix fetches through this local binary-cache endpoint, and
        publication to the LAN is an operator decision (Caddy tcp-gateway
        ingress), not something the module opens in the firewall.
      '';
    };

    port = mkOption {
      type = types.port;
      description = "TCP port atticd listens on (loopback).";
    };

    dataRoot = mkOption {
      type = types.path;
      default = "/var/lib/attic";
      description = ''
        Persistent root for attic's SQLite database and local NAR storage
        (disposable cache; loss only causes a rebuild/refetch). The service
        writes here through ReadWritePaths; the path must live on persistent
        storage so the cache survives an ephemeral root wipe.
      '';
    };

    runtimeDirectory = mkOption {
      type = types.str;
      default = "attic";
      description = "systemd RuntimeDirectory name (private /run dir).";
    };

    cacheName = mkOption {
      type = types.str;
      description = "Name of the Attic cache (the node's own binary cache).";
    };

    # JWT admin-token signing secret. atticd administers caches/tokens via
    # signed JWTs; the private half of this secret signs those tokens and must
    # never leave the server. It is injected at runtime via LoadCredential,
    # never via argv or the Nix store.
    tokenSecretFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Runtime path (e.g. an agenix secret) to a file containing either
          - ATTIC_SERVER_TOKEN_RS256_SECRET_BASE64="<base64 pem>", or
          - ATTIC_SERVER_TOKEN_HS256_SECRET_BASE64="<base64 hmac>"
        (an EnvironmentFile, exactly what attic's upstream NixOS module
        expects). Mounted via systemd LoadCredential, never passed in argv.
        When null, JWT token generation is unavailable (admin via a separately
        deployed token), but pull/push of signed nars still works.
      '';
    };

    # The Nix cache public signing key that clients must trust.
    trustedPublicKey = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = ''
        The cache's public signing key in canonical Nix form
        (`<keyName>:<base64>`), e.g. `mycache:AbCd...`. This is the public half
        of the keypair attic generates and stores server-side for the cache. It
        must equal the value reported by `attic cache info`; the client wiring
        below refuses to configure a substituter without it. The private half
        never leaves the server.
      '';
    };

    publicUrl = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = ''
        Canonical HTTP(S) URL clients should use for this cache (without the
        trailing cache name), e.g. `https://cache.lattice.local/`. When null,
        the module exposes the loopback URL `http://<host>:<port>` (intended
        for the node's own nix fetching locally, not for LAN clients).
      '';
    };

    allowUnauthenticatedRead = mkOption {
      type = types.bool;
      default = true;
      description = ''
        Whether unauthenticated clients may pull (read) from the cache. The
        cache is created `--public` when true; the node's own nix substitutes
        through it without a token. Pushing always requires a token.
      '';
    };

    # Whether to push this cache's nars as the node builds them (experimental).
    # We keep this as an option so operators can batch upload manually, but the
    # default is manual push (no daemonized uploader in pinned attic).
    substituter = mkOption {
      type = types.bool;
      default = true;
      description = ''
        Whether to register the local attic as a nix substituter for the node
        (add it to nix.settings.substituters and trusted-public-keys) so the
        node's own nix fetches paths it previously pushed through the local
        cache. Requires trustedPublicKey to be set.
      '';
    };

    # Internal: path to the generated (secret-free) server TOML, for tests and
    # debugging.
    configFile = mkOption {
      type = types.path;
      internal = true;
      description = "Path to the generated secret-free atticd TOML config.";
    };
  };
}
