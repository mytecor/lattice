{ nixpkgs, pkgs, piModule }:

# Проверка декларативного `~/.pi/agent/models.json` из lattice.pi.
#
# Гейтвея ladder: роуты standard/stupid мапятся на text-only модели
# (deepseek-ai/DeepSeek-V4-Flash-0731 и MiniMaxAI/MiniMax-M2.7), и upstream
# отвечает 400 на `image_url` (not multimodal / at most 5 images / 413 body
# too large). Поэтому Pi обязан считать эти логические модели text-only:
# `modelOverrides.<model>.input == ["text"]`. Без этого Pi дефолтит вход как
# `["text","image"]` и начинает слать в вышестоящий шлюз изображения.
#
# Регрессия, которую ловит тест: кто-то удалит `input = [ "text" ]` из
# modelOverrides — Pi снова посчитает модели мультимодальными и сессии,
# читающие картинки через read, начнут падать с 400 на роутах standard/stupid.
let
  lib = nixpkgs.lib;
  config = (lib.nixosSystem {
    modules = [
      piModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.pi = {
          enable = true;
          user = "root";
          settings = {
            defaultProvider = "llm-gateway";
            defaultModel = "standard";
            defaultThinkingLevel = "xhigh";
          };
          models.llm-gateway = {
            baseUrl = "http://127.0.0.1:9208/v1";
            api = "openai-completions";
            apiKey = "lattice-loopback-gateway";
            discoverModels = false;
            models = [ { id = "standard"; } { id = "stupid"; } ];
            modelOverrides = {
              standard = {
                # text-only: standard роутится на text-only DeepSeek-V4-Flash-0731.
                input = [ "text" ];
                thinkingLevelMap = {
                  off = null; minimal = null; low = null; medium = null;
                  high = null; xhigh = null; max = null;
                };
                compat = { supportsReasoningEffort = false; };
              };
              stupid = {
                input = [ "text" ];
                thinkingLevelMap = {
                  off = null; minimal = null; low = null; medium = null;
                  high = null; xhigh = null; max = null;
                };
                compat = { supportsReasoningEffort = false; };
              };
            };
          };
        };
      }
    ];
  }).config;

  modelsJson = config.lattice.pi.generatedModelsJson;
in
pkgs.runCommand "pi-models-config-evaluation" {
  inherit modelsJson;
  nativeBuildInputs = [ pkgs.jq ];
} ''
  set -euo pipefail

  # Семантическая проверка: у standard и stupid input == ["text"] (не image).
  # Роуты стандарт/глупый замаплены на text-only модели (DeepSeek-V4-Flash-0731,
  # MiniMax-M2.7), и без text-only Pi начнёт слать image_url → 400 upstream.
  jq -e '
    .providers["llm-gateway"].modelOverrides.standard.input == ["text"] and
    .providers["llm-gateway"].modelOverrides.stupid.input == ["text"]
  ' "$modelsJson" > "$out"
''
