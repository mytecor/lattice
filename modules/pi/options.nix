{ lib, ... }:

let
  inherit (lib) mkOption mkEnableOption types;
in
{
  options.lattice.pi = {
    enable = lib.mkEnableOption "Pi coding agent";

    user = mkOption {
      type = types.str;
      default = "root";
      description = "User whose ~/.pi/agent config is materialized by this module.";
    };

    # settings.json — глобальные настройки Pi. Полностью декларативно; ключи
    # валидируются на уровне типа-структуры, чтобы не гонять произвольный JSON.
    settings = mkOption {
      type = types.submodule {
        options.defaultProvider = mkOption {
          type = types.str;
          default = "llm-gateway";
        };
        options.defaultModel = mkOption {
          type = types.str;
          default = "standard";
        };
        options.defaultThinkingLevel = mkOption {
          type = types.enum [ "off" "minimal" "low" "medium" "high" "xhigh" "max" ];
          default = "xhigh";
        };
        options.theme = mkOption {
          type = types.nullOr types.str;
          default = null;
        };
        # Pi packages (extensions/skills/themes): строка-спека (`npm:`/`git:`/URL,
        # версия/реф в спеке — pin) либо Nix-пакет, собранный через pnpm builder
        # (в JSON попадает его store-path — локальный pi-package без runtime-загрузок).
        options.packages = mkOption {
          type = types.listOf (types.either types.str types.package);
          default = [ ];
          example = [ "npm:pi-mcp-adapter@2.33.0" ];
          description = "Pi packages, e.g. `npm:pi-mcp-adapter@2.33.0` (pinned spec) or a Nix-built package.";
        };
        # Прямая загрузка расширений по пути: локальный файл `*.ts`/`*.js` или
        # каталог расширения (index.ts). Значение-строка идёт как есть; Nix-пакет
        # (например симлинк-директория на установленный пакет) — как store-path.
        options.extensions = mkOption {
          type = types.listOf (types.either types.str types.package);
          default = [ ];
          example = [ "/path/to/extension.ts" ];
          description = "Extension files or directories loaded directly, e.g. a Nix-built extension package.";
        };
      };
      default = { };
      description = "Declarative contents of ~/.pi/agent/settings.json";
    };

    # models.json — объявление providers и моделей. Секреты в store не попадают:
    # значения apiKey/headers ссылаются на env vars (см. Pi value resolution),
    # поэтому либо отсутствуют, либо используют форму "$VAR".
    models = mkOption {
      type = types.attrsOf (types.submodule {
        options = {
          baseUrl = mkOption { type = types.str; };
          api = mkOption {
            type = types.enum [ "openai-completions" "openai-responses" "anthropic-messages" "google-generative-ai" ];
            default = "openai-completions";
          };
          apiKey = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = ''
              Secret-free value: omit, a literal, or an env/command reference ('$VAR', '!cmd').
              Real credentials are never literals; a literal is only appropriate as a "placeholder
              but non-empty" value so Pi resolves the provider's models at all — required for a
              keyless loopback gateway that ignores the Bearer (see mytecor-homelab).
            '';
          };
          discoverModels = mkOption {
            type = types.bool;
            default = false;
            description = "Disable by default; f8-02 requires only explicit logical classes.";
          };
          models = mkOption {
            type = types.listOf (types.submodule {
              options.id = mkOption { type = types.str; };
              options.name = mkOption { type = types.nullOr types.str; default = null; };
              options.reasoning = mkOption { type = types.bool; default = false; };
              options.thinkingLevelMap = mkOption {
                type = types.nullOr (types.attrsOf (types.nullOr types.str));
                default = null;
              };
              options.compat = mkOption {
                type = types.nullOr (types.attrsOf types.bool);
                default = null;
              };
            });
            default = [ ];
          };
          modelOverrides = mkOption {
            type = types.attrsOf types.attrs;
            default = { };
          };
        };
      });
      default = { };
      description = "Declarative contents of ~/.pi/agent/models.json (providers mapping).";
    };

    # --- f8-03: reproducible tool profile ---------------------------------

    # Дополнительные tools поверх базового контракта. Значения — имена атрибутов
    # `pkgs` (например `"nodejs"`) или package-значения/прямые store-пути.
    # Секретов и provider-specific настроек здесь нет.
    tools = mkOption {
      type = types.listOf (types.either types.str types.package);
      default = [ ];
      description = "Extra tools added on top of the reproducible f8-03 base tool profile.";
    };

    # Генерация /etc/pi.env — документированный, инспектируемый контракт
    # окружения (PATH, locale, git identity boundary, рабочие каталоги).
    envContract = lib.mkEnableOption "generation of /etc/pi.env environment contract" // {
      default = true;
    };

    # Read-only outputs: готовые JSON в store, на которые модуль цепляет симлинки.
    generatedSettingsJson = mkOption {
      type = types.path;
      readOnly = true;
      description = "Generated ~/.pi/agent/settings.json in the Nix store.";
    };
    generatedModelsJson = mkOption {
      type = types.path;
      readOnly = true;
      description = "Generated ~/.pi/agent/models.json in the Nix store.";
    };
    # Необязательный вывод: итоговый состав tool profile (base + tools) как пакет.
    toolProfile = mkOption {
      type = types.nullOr types.package;
      readOnly = true;
      description = "Derivation combining the base tool set with lattice.pi.tools.";
    };

  };
}
