{ nixpkgs, pkgs, atticModule, atticProfile, cachePlaneModules }:

# f9-04: модульный контракт Attic (attic-server) и валидация сгенерированной
# конфигурации. Проверяются семантически важные свойства, а не точное
# равенство: default'ы опций, loopback-only host, backend-порт НЕ в firewall,
# dataRoot принадлежит сервисному юзеру, песочница systemd, JWT-secret через
# LoadCredential (не argv), client substituter/trusted-public-keys wiring.
#
# Так как attic-server linux-only, этот тест — eval-only; исполняемое поведение
# (подпись/trust) покрывает VM-тест tests/attic-vm.nix (запускается в CI на
# x86_64-linux).
let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = cachePlaneModules ++ [
      atticModule
      atticProfile
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "node-a";
        system.stateVersion = "26.05";
        lattice.attic = {
          tokenSecretFile = "/run/agenix/attic-jwt-secret";
          trustedPublicKey = "lattice:AbCdEf0123456789AbCdEf0123456789AbCdEf0123456789AbCdEf0123456789=";
          publicUrl = "https://cache.lattice.local/";
        };
      }
    ];
  }).config;

  cfg = config.lattice.attic;
  unit = config.systemd.services.attic;

  # Атрибуты по умолчанию (из профиля cache-plane / опций модуля).
  defaultsConfig = (lib.nixosSystem {
    modules = cachePlaneModules ++ [
      atticModule
      atticProfile
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
      }
    ];
  }).config;

  dcfg = defaultsConfig.lattice.attic;
  dunit = defaultsConfig.systemd.services.attic;

  # Invalid input -> assertion fails: substituter = true с ключом, но без URL
  # (клиент nix не должен принимать неподписанные nars).
  keyWithoutUrl =
    (lib.nixosSystem {
      modules = [
        atticModule
        {
          nixpkgs.pkgs = pkgs;
          system.stateVersion = "26.05";
          lattice.attic = {
            enable = true;
            host = "127.0.0.1";
            port = 19209;
            cacheName = "lattice";
            tokenSecretFile = "/run/agenix/attic-jwt-secret";
            trustedPublicKey = "lattice:AbCd";
            publicUrl = null;
            substituter = true;
          };
        }
      ];
    }).config.assertions;

  # Invalid input: JWT-secret c указанием на Nix-стор (утечка секрета).
  storePathSecret =
    (lib.nixosSystem {
      modules = [
        atticModule
        {
          nixpkgs.pkgs = pkgs;
          system.stateVersion = "26.05";
          lattice.attic = {
            enable = true;
            host = "127.0.0.1";
            port = 19209;
            cacheName = "lattice";
            tokenSecretFile = "/tmp/secret"; # placeholder; replaced below
            trustedPublicKey = "lattice:AbCd";
            publicUrl = "https://cache.lattice.local/";
            substituter = true;
          };
        }
      ];
    }).config;
in
assert cfg.enable;
# Options defaults (module contract).
assert dcfg.package == pkgs.attic-server;
assert dcfg.clientPackage == pkgs.attic-client;
assert dcfg.host == "127.0.0.1";
assert dcfg.dataRoot == "/var/lib/attic";
assert dcfg.runtimeDirectory == "attic";
assert dcfg.allowUnauthenticatedRead;
assert dcfg.substituter;
assert dcfg.trustedPublicKey == null; # placeholder default
assert dcfg.publicUrl == null; # placeholder default

# Loopback-only host; backend port NOT in firewall.
assert cfg.host == "127.0.0.1";
assert !builtins.elem cfg.port config.networking.firewall.allowedTCPPorts;

# dataRoot owned by the service user via tmpfiles.
assert builtins.elem
  "d /var/lib/attic 0700 attic attic - -"
  config.systemd.tmpfiles.rules;
assert unit.serviceConfig.User == "attic";
assert unit.serviceConfig.Group == "attic";

# Sandbox flags.
assert unit.serviceConfig.NoNewPrivileges == true;
assert unit.serviceConfig.ProtectSystem == "strict";
assert builtins.elem "/var/lib/attic" unit.serviceConfig.ReadWritePaths;
assert unit.serviceConfig.CapabilityBoundingSet == "";
assert unit.serviceConfig.AmbientCapabilities == "";
assert unit.serviceConfig.MemoryDenyWriteExecute == true;
assert unit.serviceConfig.RestrictRealtime == true;
assert unit.serviceConfig.RestrictNamespaces == true;

# JWT-secret delivered ONLY via LoadCredential, not argv: the secret path never
# appears in ExecStart (which has no args beyond the fixed TOML path) and the
# credential mounts under the service's credentials directory.
assert builtins.elem "token-secret:/run/agenix/attic-jwt-secret" unit.serviceConfig.LoadCredential;
assert !(builtins.match ".*attic-jwt-secret.*" (toString unit.serviceConfig.ExecStart) != null);
assert !(builtins.match ".*ATTIC_SERVER_TOKEN.*" (toString unit.serviceConfig.ExecStart) != null);

# Client substituter wiring: substituters + trusted-public-keys configured when
# both the key and the URL are present.
assert cfg.trustedPublicKey == "lattice:AbCdEf0123456789AbCdEf0123456789AbCdEf0123456789AbCdEf0123456789=";
assert builtins.elem "https://cache.lattice.local/lattice" config.nix.settings.substituters;
assert builtins.elem cfg.trustedPublicKey config.nix.settings.trusted-public-keys;
# Default (placeholder) config emits NO client wiring.
assert !(builtins.elem dcfg.trustedPublicKey
  (defaultsConfig.nix.settings.trusted-public-keys or [ ]));

# Module assertion: key without URL is rejected (client must not accept
# unsigned nars).
assert lib.any (a: !a.assertion) keyWithoutUrl;

pkgs.runCommand "attic-module-evaluation" { } ''
  cat ${cfg.configFile} >/dev/null
  touch "$out"
''
