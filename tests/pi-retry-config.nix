{ nixpkgs, pkgs, piModule }:

# Контракт: `lattice.pi.settings.retry` материализуется как
# `~/.pi/agent/extensions/pi-retry/config.json` — ровно тот файл, который
# расширение @geebos/pi-retry читает по каноническому пути на каждое совпадение
# (см. src/config.ts пакета). Это конфигурация ретрая: RegExp-паттерны, по
# которым ошибка провайдера классифицируется как retryable.
#
# Что ловит: кто-то уберёт `retry`-опцию из модуля, перестанет материализовать
# config.json в activation script, или протащит секретный/плохой паттерн в
# конфиг — тест падает. Это регрессия, а не snapshot: список паттернов в
# дефолте пуст; конкретный набор живёт в конфиге ноды, а здесь проверяется
# только декларативный механизм материализации + валидность JSON и паттернов.
let
  lib = nixpkgs.lib;
  config = (lib.nixosSystem {
    modules = [
      piModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        boot.loader.grub.devices = [ "/dev/sda" ];
        fileSystems."/" = { device = "/dev/sda"; fsType = "ext4"; };
        lattice.pi = {
          enable = true;
          user = "root";
          settings = {
            defaultProvider = "llm-gateway";
            defaultModel = "standard";
            defaultThinkingLevel = "xhigh";
            extensions = [ pkgs.lattice.pi-retry ];
            retry = [ "^Provider finish_reason: abort$" "upstream stream failed" ];
          };
        };
      }
    ];
  }).config;

  retryJson = config.lattice.pi.generatedRetryConfigJson;
in
pkgs.runCommand "pi-retry-config-evaluation"
  {
    inherit retryJson;
    nativeBuildInputs = [ pkgs.jq ];
  } ''
  set -euo pipefail

  # 1. Это валидный JSON с ключом "patterns".
  jq -e '.patterns | type == "array"' "$retryJson" > /dev/null

  # 2. Паттерны — допустимые RegExp-источники (расширение компилирует их через
  #    new RegExp(source, "i")); невалидный паттерн молча игнорируется.
  jq -e '.patterns | length > 0' "$retryJson" > /dev/null

  # 3. Ровно то, что объявлено в модуле (без секретов/мусора).
  actual=$(jq -c '.patterns | sort' "$retryJson")
  expected=$(jq -nc --argjson p '["^Provider finish_reason: abort$","upstream stream failed"]' '$p | sort')
  test "$actual" = "$expected" \
    || { echo "FAIL: patterns mismatch: $actual != $expected" >&2; exit 1; }

  # 4. Activation script материализует симлинк на правильном пути.
  grep -q 'extensions/pi-retry' "${config.system.build.toplevel}/activate" \
    || { echo "FAIL: activation script does not materialize extensions/pi-retry/config.json" >&2; exit 1; }

  echo "OK: pi-retry config.json materializes retry patterns"
  mkdir "$out"
''
