{ buildPnpmCli, lib, runCommand }:

# pi-retry — Pi-расширение для классификации provider-specific и stalled-stream
# ошибок как retryable, с конфигурируемыми паттернами ретрая (@geebos/pi-retry).
#
# Полная сборка через buildPnpmCli (offline в Nix sandbox; pnpm-lock.yaml
# закрепляет dependency graph и integrity; package не является CLI — у него нет
# bin, поэтому executables пуст и versionCheck отключён). pnpm 11 supply-chain
# политики (minimumReleaseAge / blockExoticSubdeps) для этого пакета отключаются
# в pnpm-workspace.yaml: для Nix-pinned lock они избыточны — integrity уже
# закреплён в store.
#
# Вывод для Pi — каталог с двумя симлинк-точками (без копирования node_modules):
# `extension` — корень установленного пакета (сюда указывает settings.extensions
# как `${p}/extension`), `node_modules` — дерево зависимостей, чтобы резолвер
# Pi/Jiti (поднимающийся текстуально от данной точки входа) находил deps: для
# точки входа вне дерева node_modules текстуальный ascent через симлинк каталога
# уходит мимо дерева, поэтому дерево дублируется симлинком на этом же уровне.
# На ноде не нужны ни node/npm, ни runtime-загрузки из npm registry.
#
# Конфигурация паттернов ретрая (config.json) за пределами package.nix: её
# материализует модуль `lattice.pi` на ноде (см. extensions/pi-retry/config.json).
let
  pkg = buildPnpmCli {
    pname = "pi-retry";
    version = "0.0.2";
    package = "@geebos/pi-retry";
    pnpmLock = ./pnpm-lock.yaml;
    pnpmDepsHash = "sha256-zq1YBcfjqk68xrFdg87BoAd6+VVm5PvFPfmooRilbIs=";
    pnpmWorkspace = ./pnpm-workspace.yaml;

    description = "Pi extension that classifies provider-specific and stalled-stream errors for retry";
    homepage = "https://github.com/geebos/pi-retry";
    license = lib.licenses.mit;

    # Не CLI: у @geebos/pi-retry нет bin-таргетов. executables пуст — builder не
    # создаёт wrappers; installCheckPhase требует CLI-вывод, поэтому отключаем.
    executables = { };
    mainProgram = "pi-retry";
    versionCheck = false;
  };
  nodeModules = "${pkg}/libexec/pi-retry/node_modules";
  packageDir = "${nodeModules}/@geebos/pi-retry";
in
runCommand "pi-retry-${pkg.version}" { } ''
  mkdir -p "$out"
  ln -s ${packageDir} "$out/extension"
  ln -s ${nodeModules} "$out/node_modules"
''
