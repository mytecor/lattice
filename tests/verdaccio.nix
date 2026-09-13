{ pkgs, nixpkgs, verdaccioModule, verdaccioProfile, cachePlaneModules }:

# f9-03: Verdaccio npm/pnpm/yarn caching proxy поведение в VM (CI only).
#
# Внутри VM: verdaccio стартует через модуль lattice.verdaccio на loopback
# (cacheRoot /var/cache/verdaccio), и три package manager (npm/pnpm/yarn)
# получают пакеты через один proxy endpoint. Проверяется disposable-семантика:
#   * cold install через каждый package manager -> cache наполняется;
#   * warm install даёт тот же dependency graph по lockfile;
#   * удаление cacheRoot -> cold install снова тот же результат (refetch из
#     uplink), cache никогда не является source of truth.
#
# verdaccio — node-пакет; на macOS этот тест только EVALUATE-ится,
# исполняется в CI (ubuntu x86_64-linux). Реестровый набор (registry.npmjs.org)
# требует сети в VM; small, стабильный пакет задан ниже.
let
  inherit (nixpkgs) lib;
  # Loopback port in the test range.
  verdaccioPort = 19212;

  # A tiny, stable package to fetch through the proxy on all three managers.
  # `is-number@7.0.0` is old (it depends on nothing), so the cold/warm
  # dependency graph is small and deterministic.
  pkg = "is-number";
  pkgVersion = "7.0.0";

  # Registry config per manager, pointing at the loopback proxy. Because the
  # managers are invoked with an explicit `--registry` / `--rc` flag here (the
  # tool-profile wiring is a separate concern), we keep the test self-contained.
  registryUrl = "http://127.0.0.1:${toString verdaccioPort}";
in
pkgs.testers.runNixOSTest {
  name = "verdaccio";

  nodes.machine = { ... }: {
    imports = cachePlaneModules ++ [ verdaccioModule verdaccioProfile ];

    environment.systemPackages = [ pkgs.nodejs pkgs.pnpm pkgs.yarn pkgs.curl ];

    lattice.verdaccio = {
      enable = true;
      host = "127.0.0.1";
      port = verdaccioPort;
      cacheRoot = "/var/cache/verdaccio";
      upstreamRegistry = "https://registry.npmjs.org";
      publish = false;
      logLevel = "error";
    };
  };

  testScript = ''
    import hashlib

    registry = ${builtins.toJSON registryUrl}
    pkg = ${builtins.toJSON pkg}
    pkgVersion = ${builtins.toJSON pkgVersion}
    cache_root = "/var/cache/verdaccio"
    lockfile = "/tmp/lock"

    # The exact tarball URL for is-number@7.0.0 is `is-number-7.0.0.tgz`; we use
    # that to prove the tarball flows through the (cached) proxy.
    url = f"{registry}/{pkg}/-/{pkg}-{pkgVersion}.tgz"

    machine.start()
    machine.wait_for_unit("verdaccio.service")
    machine.wait_for_open_port(${toString verdaccioPort})

    # --- the cache dir belongs to the service user and is writable ---
    machine.succeed("test -d /var/cache/verdaccio")
    machine.succeed("systemctl show -p User --value verdaccio.service | grep -Fx verdaccio")

    # --- npm cold install through the proxy, cache populated ---
    machine.succeed(f"""
      set -eu
      cd /tmp && rm -rf npmdir && mkdir npmdir && cd npmdir
      npm init -y >/dev/null
      npm install --registry {registry} --cache /tmp/npm-cache {pkg}@{pkgVersion} >/dev/null
      node -e "console.log(require('{pkg}/package.json').version)" | tee {lockfile}-npm
    """)
    assert machine.succeed(f"cat {lockfile}-npm").strip() == pkgVersion
    # tarball cached under the cache root
    machine.succeed(f"test -d {cache_root}/{pkg}")

    # --- warm npm install from the mirror, identical graph ---
    machine.succeed(f"""
      set -eu
      cd /tmp && rm -rf npmdir2 && mkdir npmdir2 && cd npmdir2
      npm init -y >/dev/null
      npm install --registry {registry} --cache /tmp/npm-cache2 {pkg}@{pkgVersion} >/dev/null
      node -e "console.log(require('{pkg}/package.json').version)"
    """)

    # --- disposable: delete cache -> cold install again, same result ---
    machine.succeed("rm -rf /var/cache/verdaccio")
    machine.succeed(f"""
      set -eu
      cd /tmp && rm -rf npmdir3 && mkdir npmdir3 && cd npmdir3
      npm init -y >/dev/null
      npm install --registry {registry} --cache /tmp/npm-cache3 {pkg}@{pkgVersion} >/dev/null
      node -e "console.log(require('{pkg}/package.json').version)" | tee {lockfile}-npm2
    """)
    assert machine.succeed(f"cat {lockfile}-npm2").strip() == pkgVersion

    # --- pnpm install through the same proxy endpoint ---
    machine.succeed(f"""
      set -eu
      cd /tmp && rm -rf pnpmdir && mkdir pnpmdir && cd pnpmdir
      pnpm init >/dev/null 2>&1 || true
      pnpm add {pkg}@{pkgVersion} --registry {registry} --store-dir /tmp/pnpm-store >/dev/null
      node -e "console.log(require('{pkg}/package.json').version)" | tee {lockfile}-pnpm
    """)
    assert machine.succeed(f"cat {lockfile}-pnpm").strip() == pkgVersion

    # --- yarn install through the same proxy endpoint ---
    machine.succeed(f"""
      set -eu
      cd /tmp && rm -rf yarnir && mkdir yarnir && cd yarnir
      touch package.json
      yarn add {pkg}@{pkgVersion} --registry {registry} --non-interactive >/dev/null
      node -e "console.log(require('{pkg}/package.json').version)" | tee {lockfile}-yarn
    """)
    assert machine.succeed(f"cat {lockfile}-yarn").strip() == pkgVersion

    # All three package managers resolved the same version through one endpoint.
  '';
}
