{ buildPnpmCli, lib, runCommand }:

# pi-mcp-adapter — MCP (Model Context Protocol) adapter extension для Pi.
#
# Полная сборка идёт через buildPnpmCli (offline в Nix sandbox; pnpm-lock.yaml
# закрепляет dependency graph и integrity; bin — CLI `pi-mcp-adapter`). pnpm 11
# supply-chain политики (minimumReleaseAge / blockExoticSubdeps) для этого
# пакета отключаются в pnpm-workspace.yaml: для Nix-pinned lock они избыточны —
# integrity уже закреплён в store.
#
# Вывод для Pi — каталог с двумя симлинк-точками (без копирования node_modules):
# `extension` — корень установленного пакета (сюда указывает settings.extensions
# как `${p}/extension`), `node_modules` — полное дерево зависимостей, чтобы
# резолвер Pi/Jiti (поднимающийся текстуально от данного пути) находил deps: для
# точки входа вне дерева node_modules текстуальный ascent через симлинк каталога
# уходит мимо дерева, поэтому дерево дублируется симлинком на этом же уровне.
# На ноде не нужны ни node/npm, ни runtime-загрузки из npm registry: расширение
# и зависимости уже лежат в Nix store.
let
  pkg = buildPnpmCli {
    pname = "pi-mcp-adapter";
    version = "2.33.0";
    package = "pi-mcp-adapter";
    pnpmLock = ./pnpm-lock.yaml;
    pnpmWorkspace = ./pnpm-workspace.yaml;
    pnpmDepsHash = "sha256-WDOZcxjOzM+mhRinjI9AVi6ZJrrEsiLhD0roo4+JyoM=";

    description = "MCP (Model Context Protocol) adapter extension for Pi coding agent";
    homepage = "https://github.com/nicobailon/pi-mcp-adapter";
    license = lib.licenses.mit;

    # CLI (`pi-mcp-adapter init/…`) из той же сборки.
    executables.pi-mcp-adapter = "cli.js";
    mainProgram = "pi-mcp-adapter";
    # У CLI нет стабильного контракта `--version`; версия закреплена lock-файлом.
    versionCheck = false;
  };
  nodeModules = "${pkg}/libexec/pi-mcp-adapter/node_modules";
  packageDir = "${nodeModules}/pi-mcp-adapter";
in
runCommand "pi-mcp-adapter-${pkg.version}" { } ''
  mkdir -p "$out"
  ln -s ${packageDir} "$out/extension"
  ln -s ${nodeModules} "$out/node_modules"
''
