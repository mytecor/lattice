# f18-07. Разделить browser runtime и Jev на два systemd сервиса

## Контекст

В homelab Camoufox не должен жить внутри процесса Jev. Перед NixOS-модулем оформляем
минимальные systemd-юниты: отдельный long-running browser runtime и отдельный сервис Jev.
Связь — строго через `BU_CDP_URL`.

## Что сделать

- [x] Сервис `foxbridge-camoufox.service`: сам Foxbridge, который поднимает Camoufox
      (Juggler backend) и слушает локальный CDP endpoint.
- [x] Сервис `jev-ultrafast.service`: Jev, подключённый через
      `BU_CDP_URL=http://127.0.0.1:<foxbridge-port>`.
- [x] Зависимость Jev от Foxbridge: `After=foxbridge-camoufox.service`,
      `Requires=foxbridge-camoufox.service`.
- [x] Foxbridge слушает только localhost либо приватный network namespace; CDP endpoint
      наружу homelab не публикуется.
- [x] Сервисы поднимаются через systemd на целевой машине (вне NixOS — руками).

## Критерий готовности (Definition of Done)

- [x] Два независимых systemd-сервиса, Jev стартует только после готовности
      `foxbridge-camoufox`.
- [x] CDP недоступен извне хоста (проверить с другой машины/интерфейса).

## Проверено на ноде 2026-09-22

- Оба юнита `active (running)`: `foxbridge-camoufox` (Main PID 1508822) и `jev-ultrafast`
  (Main PID 1517932, `Jev Ultrafast: http://127.0.0.1:8766`).
- Jev стартовал только после готовности рантайма: `ExecStartPre` дождался
  `curl http://127.0.0.1:9222/json/version` (`foxbridge/1.0`), потом поднялся сам.
- Связь строго через CDP-сокет: `/json/version` → `webSocketDebuggerUrl`
  `ws://127.0.0.1:9222/devtools/browser/foxbridge`; инспектор Jev отдаёт
  `{"text_model":"deepseek-chat","status":"idle"}`.
- CDP недоступен извне хоста: Foxbridge слушает только `127.0.0.1:9222` (`ss -tlnp`),
  а с рабочей машины на `192.168.3.12:9222`, `192.168.60.184:9222` и ygg-адрес
  `200:e9f0:…:fb91:9222` порт закрыт (REFUSED/TIMEOUT) — на всех интерфейсах узла,
  включая loopback-only биндинг.

## Найденные при отладке нюансы окружения (учесть в f18-08)

1. **No `/bin/bash` на NixOS**: у launcher'а shebang `#!/usr/bin/env bash` не сработал
   под systemd (`env: 'bash': No such file or directory`). Скрипт POSIX-совместим —
   вызвали через `/bin/sh`, явно. В NixOS-модуле `exec` напрямую скрипт/binary, без
   shebang-зависимости.
2. **systemd усекает окружение**: `curl`/`sleep` в `ExecStartPre` и `PATH` для бинаря
   не находятся без `Environment=PATH=…` (нет `/bin/sleep`). Оба юнита получили явный
   `Environment=PATH=/root/.nix-profile/bin:/nix/profile/bin:/run/current-system/sw/bin:/usr/bin:/bin`.
3. **Camoufox content-sandbox под systemd падает SEGV** (forkserver coredump), но
   *только в строгом санбокс-окружении*: вручную тот же launcher работает. Отключили
   `MOZ_DISABLE_CONTENT_SANDBOX=1`. Для f18-08: проверять в NixOS-производном, не
   тащить `LD_LIBRARY_PATH`-glob.

## Ответы на открытые вопросы

## Затрагиваемые файлы / слои

- Пока вне NixOS: systemd unit-файлы в репозитории фичи (черновик для f18-08).
- `profiles/`/`nodes/` не трогаем до f18-08.

## Открытые вопросы

_нет_
