{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.pi;

  # Оставляем из настроек только не-null и включённые поля, чтобы не раздувать JSON.
  # Упаковка pi-package: строка-спека проходит как есть, Nix-пакет — как store-path.
  renderPackage = p: if lib.isString p then p else toString p;

  # Расширение-пакет: точка входа для Pi — `${p}/extension` (симлинк-каталог на
  # установленный пакет) при наличии `node_modules`-симлинка рядом; см. пакет.
  renderExtension = e: if lib.isString e then e else "${e}/extension";

  settingsJson = pkgs.writeText "pi-settings.json" (builtins.toJSON (
    (lib.filterAttrs (name: value: value != null) {
      defaultProvider = cfg.settings.defaultProvider;
      defaultModel = cfg.settings.defaultModel;
      defaultThinkingLevel = cfg.settings.defaultThinkingLevel;
      inherit (cfg.settings) theme;
    })
    // (lib.optionalAttrs (cfg.settings.packages != []) {
        packages = map renderPackage cfg.settings.packages;
      })
    // (lib.optionalAttrs (cfg.settings.extensions != []) {
        extensions = map renderExtension cfg.settings.extensions;
      })
  ));

  # providers → models.json. Секреты не появляются здесь: apiKey подаётся как
  # env/command reference либо вообще отсутствует.
  renderModel = model: (lib.filterAttrs (k: v: v != null) { inherit (model) id; })
    // (lib.optionalAttrs (model.name != null) { name = model.name; })
    // (lib.optionalAttrs model.reasoning { reasoning = true; })
    // (lib.optionalAttrs (model.thinkingLevelMap != null) { thinkingLevelMap = model.thinkingLevelMap; })
    // (lib.optionalAttrs (model.compat != null) { compat = model.compat; });

  providerJson = name: provider: (lib.filterAttrs (k: v: v != null) {
    inherit (provider) baseUrl api discoverModels;
  } // (lib.optionalAttrs (provider.apiKey != null) { apiKey = provider.apiKey; })
    // (lib.optionalAttrs (provider.models != []) { models = map renderModel provider.models; })
    // (lib.optionalAttrs (provider.modelOverrides != { }) { modelOverrides = provider.modelOverrides; }));

  modelsJson = pkgs.writeText "pi-models.json" (builtins.toJSON {
    providers = lib.mapAttrs providerJson cfg.models;
  });

  # База .pi/agent для целевого пользователя: раскрываем $HOME через getent.
  homeFromUser = builtins.toString (config.users.users.${cfg.user}.home or "/root");

  # --- f8-03: reproducible tool profile ------------------------------------
  # Единый базовый контракт bash/git/tools. Один источник и для системного
  # профиля, и для flake devShell — интерактивная нода и будущий worker
  # получают одинаковый набор, а проектные зависимости добавляются сверху
  # (`lattice.pi.tools` на ноде, packages в devShell), не меняя рантайм Pi.
  baseTools = (import ../../profiles/pi/base-tools.nix { inherit pkgs; }).base;

  # Нормализация значений `lattice.pi.tools`: строки — имена атрибутов `pkgs`,
  # package-значения используются как есть.
  resolveTool = tool:
    if lib.isString tool then
      pkgs.${tool}
    else
      tool;
  extraTools = map resolveTool cfg.tools;

  # Итоговый состав tool profile: базовый контракт + расширение проекта.
  toolProfile = pkgs.buildEnv {
    name = "lattice-pi-tool-profile";
    paths = baseTools ++ extraTools;
  };

  # PATH из одного места: тот же список, что и в systemPackages, но как
  # bin-path для контракта /etc/pi.env.
  toolBinPath = lib.makeBinPath (baseTools ++ extraTools);

  gitConfigHome = "${homeFromUser}/.gitconfig";
in
{
  config = lib.mkIf cfg.enable {
    lattice.pi.generatedSettingsJson = settingsJson;
    lattice.pi.generatedModelsJson = modelsJson;

    # f8-03: на ноде есть ровно декларированный tool profile (базовый контракт
    # + `lattice.pi.tools`) — без зависимости от случайных user/global пакетов.
    environment.systemPackages = [ pkgs.lattice.pi toolProfile ];

    # f8-03: документированный, инспектируемый контракт окружения рантайма —
    # PATH, locale, git identity boundary и рабочие каталоги. Файл read-only;
    # не meant to be sourced пользовательскими оболочками.
    environment.etc."pi.env" = lib.mkIf cfg.envContract {
      text = ''
        # Pi runtime environment contract (f8-03). Read-only, inspect only.
        export PATH=${toolBinPath}$''${PATH:+:$PATH}
        export LANG=C.UTF-8
        export LC_ALL=C.UTF-8
        export GIT_CONFIG_NOSYSTEM=1
        export GIT_CONFIG_GLOBAL=${gitConfigHome}
      '';
    };

    system.activationScripts.pi-config = lib.stringAfter [ "users" ] ''
      set -eu
      home=${lib.escapeShellArg homeFromUser}
      pi_dir="$home/.pi/agent"
      install -d -m 0700 "$pi_dir"
      ln -sfn ${settingsJson} "$pi_dir/settings.json"
      ln -sfn ${modelsJson} "$pi_dir/models.json"
    '';

    # Read-only вывод: готовый tool profile как derivation (для инспекции/тестов).
    lattice.pi.toolProfile = toolProfile;
  };
}
