{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.pi;

  # Оставляем из настроек только не-null и включённые поля, чтобы не раздувать JSON.
  settingsJson = pkgs.writeText "pi-settings.json" (builtins.toJSON (
    lib.filterAttrs (name: value: value != null) {
      defaultProvider = cfg.settings.defaultProvider;
      defaultModel = cfg.settings.defaultModel;
      defaultThinkingLevel = cfg.settings.defaultThinkingLevel;
      inherit (cfg.settings) theme;
    }
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
in
{
  config = lib.mkIf cfg.enable {
    lattice.pi.generatedSettingsJson = settingsJson;
    lattice.pi.generatedModelsJson = modelsJson;

    environment.systemPackages = [ pkgs.lattice.pi ];

    system.activationScripts.pi-config = lib.stringAfter [ "users" ] ''
      set -eu
      home=${lib.escapeShellArg homeFromUser}
      pi_dir="$home/.pi/agent"
      install -d -m 0700 "$pi_dir"
      ln -sfn ${settingsJson} "$pi_dir/settings.json"
      ln -sfn ${modelsJson} "$pi_dir/models.json"
    '';
  };
}
