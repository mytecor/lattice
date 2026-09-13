{ pkgs, nixpkgs, atticModule, atticProfile, cachePlaneModules }:

# f9-04: Attic binary cache поведение в VM (CI only, x86_64-linux).
#
# Внутри VM: atticd стартует через модуль lattice.attic, кеш создаётся с
# JWT-secret, строится derivation, nar загружается, и проверяется
# trust-контракт:
#   * клиент с правильным trusted-public-key substitute'ит nar;
#   * клиент без trust-ключа отвергает nar (signature verification failed);
#   * удаление данных attic (dataRoot) -> rebuild/refetch, но тот же результат
#     (disposable-семантика).
#
# attic-server linux-only: на macOS этот тест только EVALUATE-ится,
# исполняется в CI (ubuntu x86_64-linux). Signing keypair генерируется
# server-side при `attic cache create` (KeypairConfig::Generate); отдельного
# бинаря keygen в pinned-версии у аттика нет.
let
  inherit (nixpkgs) lib;
  # Loopback port in the test range.
  atticPort = 19209;

  # HS256 JWT admin secret (test-only; in production this is an age secret).
  jwtB64 = "bXktdGVzdC1hdHRpYy1qd3Qtc2VjcmV0LWJhc2U2NA=="; # "my-test-attic-jwt-secret-base64"

  # EnvironmentFile fragment the daemon reads from its credential.
  jwtSecret = pkgs.writeText "attic-jwt-secret.env" ''
    ATTIC_SERVER_TOKEN_HS256_SECRET_BASE64="${jwtB64}"
  '';

  # A small derivation "artifact" to push into the cache.
  probe = pkgs.runCommand "attic-probe" { } ''
    mkdir -p $out
    echo "attic probe content" > $out/probe.txt
  '';
in
pkgs.testers.runNixOSTest {
  name = "attic";

  nodes.machine = { ... }: {
    imports = cachePlaneModules ++ [ atticModule atticProfile ];

    environment.systemPackages = [ pkgs.attic-client pkgs.curl pkgs.jq pkgs.python3 ];

    lattice.attic = {
      enable = true;
      host = "127.0.0.1";
      port = atticPort;
      cacheName = "lattice";
      # Runtime path (not a Nix store path, so the module's store-directed
      # `tokenSecretFile` assertion holds). The setup unit below materialises
      # the test secret there before attic starts. In production this path is
      # an agenix-decrypted age secret instead.
      tokenSecretFile = "/run/attic-test/jwt-secret.env";
      allowUnauthenticatedRead = true;
      # Client wiring is exercised explicitly below (with/without the trust
      # key), so the module-level default wiring stays off.
      substituter = false;
      trustedPublicKey = null;
      publicUrl = null;
    };

    # Materialise the (test-only) JWT env file outside the Nix store, mirroring
    # how an agenix secret would be mounted in production. Copies from the
    # store-path artifact to a runtime /run path with strict permissions before
    # the attic service starts.
    systemd.services.lattice-attic-test-env = {
      description = "Materialise attic test JWT secret";
      wantedBy = [ "multi-user.target" ];
      before = [ "attic.service" ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
      };
      script = ''
        mkdir -p /run/attic-test
        install -m 0600 -o attic -g attic ${toString jwtSecret} \
          /run/attic-test/jwt-secret.env
      '';
    };
  };

  testScript = ''
    import base64
    import hashlib
    import hmac
    import json
    import time

    attic = "${lib.getExe' pkgs.attic-client "attic"}"
    atticd = "${lib.getExe' pkgs.attic-server "atticd"}"
    probe_path = ${builtins.toJSON (builtins.toString probe)}
    port = ${toString atticPort}
    base = f"http://127.0.0.1:{port}"
    cache_url = f"{base}/lattice"
    secret_b64 = ${builtins.toJSON jwtB64}

    def mint_token():
        # attic's HS256 JWT: {sub, exp (sec), nbf=iat, custom =>
        # "https://jwt.attic.rs/v1": {"caches": {"*": {perms: 1}}}}
        now = int(time.time())
        claims = {
            "sub": "root",
            "exp": now + 3600,
            "nbf": now - 60,
            "https://jwt.attic.rs/v1": {
                "caches": {
                    "*": {"r": 1, "w": 1, "d": 1, "cc": 1, "cr": 1, "cq": 1, "cd": 1}
                }
            },
        }
        def b64(b):
            return base64.urlsafe_b64encode(b).rstrip(b"=").decode()
        header = b64(json.dumps({"alg": "HS256", "typ": "JWT"}).encode())
        payload = b64(json.dumps(claims).encode())
        secret = base64.b64decode(secret_b64)
        signing_input = f"{header}.{payload}".encode()
        sig = b64(hmac.new(secret, signing_input, hashlib.sha256).digest())
        return f"{header}.{payload}.{sig}"

    machine.start()
    machine.wait_for_unit("attic.service")
    machine.wait_for_open_port(port)

    token = mint_token()

    # --- log in (writes client config) and create the cache (public) ---
    machine.succeed(f'''
      set -eu
      export HOME=/root
      {attic} login lattice {base} {token}
      {attic} cache create lattice --public
    ''')

    # --- read back the public signing key ---
    public_key = machine.succeed(
        f"export HOME=/root && {attic} cache info lattice | sed -n 's/^ *Public Key: //p'"
    ).strip()
    assert ":" in public_key, f"bad public key: {public_key!r}"

    # --- push the built derivation into the cache ---
    machine.succeed(f'''
      set -eu
      export HOME=/root
      {attic} push lattice {probe_path}
    ''')

    # --- trust contract: client WITH the public key substitutes, in a fresh
    #     store (so the path is not already present) ---
    machine.succeed(f'''
      set -eu
      rm -rf /tmp/store-trusted
      nix-store --store /tmp/store-trusted -r {probe_path} \
        --option substituters {cache_url} \
        --option trusted-public-keys {public_key}
    ''')
    got = machine.succeed(f"cat /tmp/store-trusted{probe_path}/probe.txt").strip()
    assert got == "attic probe content", got

    # --- trust contract: client WITHOUT the trust key rejects the nar ---
    out = machine.fail(f'''
      rm -rf /tmp/store-untrusted
      nix-store --store /tmp/store-untrusted -r {probe_path} \
        --option substituters {cache_url} \
        --option trusted-public-keys "bogus:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
    ''')
    assert any(k in out.lower() for k in
               ["signature", "trusted-public-keys", "unsigned", "not trusted"]), out

    # --- disposable semantics: wipe attic data -> recreate + refetch, same
    #     result (cache is not source of truth; content reproducible) ---
    new_key = machine.succeed(f'''
      set -eu
      export HOME=/root
      systemctl stop attic.service
      rm -rf /var/lib/attic
      systemctl start attic.service
      sleep 1
      {attic} login lattice {base} {token}
      {attic} cache create lattice --public
      {attic} push lattice {probe_path}
      {attic} cache info lattice | sed -n 's/^ *Public Key: //p'
    ''').strip()
    # After a wipe the keypair is regenerated, so the previously recorded key
    # no longer verifies; prove the NEW key works and yields the same content.
    assert ":" in new_key
    machine.succeed(f'''
      set -eu
      rm -rf /tmp/store-after-wipe
      nix-store --store /tmp/store-after-wipe -r {probe_path} \
        --option substituters {cache_url} \
        --option trusted-public-keys {new_key}
    ''')
    got = machine.succeed(f"cat /tmp/store-after-wipe{probe_path}/probe.txt").strip()
    assert got == "attic probe content", got
  '';
}
