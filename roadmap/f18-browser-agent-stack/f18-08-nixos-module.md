# f18-08. Декларативный NixOS-модуль

## Контекст

После рабочего PoC (f18-07) оформить стек декларативно в NixOS: два модуля в `modules/`,
секреты — через существующий механизм homelab (agenix), никогда не в Nix store.

## Что сделать

- [ ] Модуль `modules/services/foxbridge-camoufox.nix` с опциями вида:
      ```nix
      services.foxbridgeCamoufox = {
        enable = true;
        listenAddress = "127.0.0.1";
        port = 9222;
        camoufox = {
          headless = true;
          humanize = true;
        };
      };
      ```
- [ ] Модуль `modules/services/jev-ultrafast.nix` с опциями вида:
      ```nix
      services.jevUltrafast = {
        enable = true;
        cdpUrl = "http://127.0.0.1:9222";
      };
      ```
- [ ] Преобразовать черновые systemd-юниты из f18-07 в модульную форму
      (`After=`/`Requires=` между сервисами, loopback-only listen).
- [ ] API keys / секреты — только через агентов homelab (agenix), в Nix store не класть.
- [ ] Подключить модули к профилю ноды, собрать и применить (декларативно).

## Критерий готовности (Definition of Done)

- [ ] После `nixos-rebuild switch` оба сервиса поднимаются автоматически, без ручных шагов.
- [ ] Никаких секретов в Nix store; CDP слушает loopback.

## Затрагиваемые файлы / слои

- `modules/services/foxbridge-camoufox.nix`, `modules/services/jev-ultrafast.nix`;
- профиль ноды (`profiles/…/config.nix`) — подключение модулей;
- секреты в `.secrets/` + agenix-конфиг (без вывода в stdout).

## Открытые вопросы

- Как именно ставится Foxbridge/Camoufox/Jev в NixOS (nodejs-пакеты через `pkgs`, pinned
  vs образы, откуда брать `browser-harness` как dependency Jev) — решается на PoC.
