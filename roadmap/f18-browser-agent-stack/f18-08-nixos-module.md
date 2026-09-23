# f18-08. Декларативный NixOS-модуль

## Контекст

После рабочего PoC (f18-07) оформить стек декларативно в NixOS: два модуля в `modules/`,
секреты — через существующий механизм homelab (agenix), никогда в Nix store.

## Что сделать

- [x] Модуль для browser runtime (`modules/services/foxbridge-camoufox/`):
      опции `lattice.foxbridge-camoufox` (enable, package, camoufoxPackage, port, user/group,
      listenAddress-loopback contract, camoufox.headless/humanize, stateDir).
- [x] Модуль для агента (`modules/services/jev-ultrafast/`): опции `lattice.jev-ultrafast`
      (enable, package, cdpUrl, inspectorPort, runtimeDirectory, textModel, textModelBaseUrl,
      typesafeApiKeyFile, textModelApiKeyFile).
- [x] Преобразовать черновые systemd-юниты из f18-07 в модульную форму: `After=`/`Requires=`
      foxbridge-camoufox.service → jev-ultrafast.service; loopback-only listen; ExecStartPre
      health-probe CDP `/json/version` в Jev.
- [x] API keys / секреты — только через agenix + `LoadCredential` (в Nix store не класть).
- [x] Подключить модули к профилю `profiles/browser-agent-stack/config.nix` и ноде
      `nodes/mytecor-homelab` (секреты условно через `builtins.pathExists`), собрать и применить
      декларативно через comin.

## Критерий готовности (Definition of Done) — подтверждено на живой ноде 2026-09-23

- [x] После `nixos-rebuild switch` (comin) оба сервиса поднимаются автоматически, без ручных шагов.
      Нода `mytecor-homelab` применила `main` (генерация от 03:48 UTC): `foxbridge-camoufox`
      `active (running)`, `jev-ultrafast` `active (running)`, NRestarts=0.
- [x] Никаких секретов в Nix store: ключи Jev монтируются через `LoadCredential` из agenix-путей
      (`/run/agenix/...`), в env не попадают (проверено — в unit только `CREDENTIALS_DIRECTORY`).
      Секрет вычитывается из credential-файла обёрткой-скриптом, никогда через argv/store.
- [x] CDP слушает loopback: `ss -tlnp` показывает `127.0.0.1:9222` (foxbridge) и `127.0.0.1:8766`
      (jev-inspector); на LAN-интерфейсах (enp3s0 192.168.3.12, wlp2s0 192.168.60.184, ygg0)
      CDP недоступен (negative test).

### Путь к стабильному состоянию (как дошли до рабочего юнита)

Финальный стабильный вид юнита достигнут серией фиксов, каждый из которых подтверждён на ноде:

1. Seccomp `~@resources` нельзя — Camoufox вызывает `setpriority` (syscall 141) на старте,
   фильтр убивает `SIGSYS/31` (commit 207b0d8). Оставлен `@system-service` allow-list +
   `~@privileged` + полное capability/namespace/fs-хардение.
2. `--profile` нельзя — Camoufox сам создаёт профиль в `$HOME/.cache/camoufox`; явный
   `--profile` на пути, который RuntimeDirectory пересоздаёт пустым при каждом старте, ломает
   Juggler Browser.enable (f39a707).
3. HOME должен быть персистентным (не tmpfs `/run`): под полным hardening Camoufox не
   инициализируется на tmpfs-home (Browser.enable stall / coredump), на `/var/lib` — работает
   с тем же hardening. Итог: `stateDir=/var/lib/foxbridge-camoufox` через tmpfiles, HOME,
   WorkingDirectory и ReadWritePaths на него (06c6d4e).
4. Применение `main` comin'ом на ноду после этих фиксов → стабильный активный стек
   (2019+ счётчик рестартов прекратился).

