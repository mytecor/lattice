{ lib, ... }:

let
  latticePorts = import ../networking/ports.nix;
in
{
  lattice.git-cache-proxy = {
    enable = lib.mkDefault true;
    host = lib.mkDefault "127.0.0.1";
    port = lib.mkDefault latticePorts.git-cache-proxy;
    upstream = lib.mkDefault "https://github.com";
    cacheRoot = lib.mkDefault "/var/cache/git-cache-proxy";
    # f9-02: repo-scoped authorization. The profile stays intentionally
    # permissive (serve anything) so the module contract and VM tests exercise
    # both modes; the production node sets the exact allowlist.
    allowRepos = lib.mkDefault [ ];
  };

  # f9-04: Attic Nix binary cache / artifact cache. Loopback-only; the node's
  # own nix substitutes through it. The signing key is generated and stored
  # server-side by attic (NOT a file); the node only ever trusts the public key
  # (trustedPublicKey), which stays a null placeholder until the operator
  # creates the cache and records its public key (see modules/attic/README).
  # The JWT admin-token secret comes from an agenix secret via LoadCredential.
  lattice.attic = {
    enable = lib.mkDefault true;
    host = lib.mkDefault "127.0.0.1";
    port = lib.mkDefault latticePorts.attic;
    cacheName = lib.mkDefault "lattice";
    dataRoot = lib.mkDefault "/var/lib/attic";
    allowUnauthenticatedRead = lib.mkDefault true;
    substituter = lib.mkDefault true;
    # Pre-deploy placeholder: no client wiring is emitted until the operator
    # sets the real public key (from `attic cache info`) and the canonical URL.
    trustedPublicKey = lib.mkDefault null;
    publicUrl = lib.mkDefault null;
  };

  lattice.verdaccio = {
    enable = lib.mkDefault true;
    host = lib.mkDefault "127.0.0.1";
    port = lib.mkDefault latticePorts.verdaccio;
    cacheRoot = lib.mkDefault "/var/cache/verdaccio";
  };
}
