# Foxbridge + Camoufox browser runtime (F18)

Модуль предоставляет `lattice.foxbridge-camoufox.enable` — long-running browser
runtime: **Camoufox** (антидетект-сборка Firefox) под управлением **Foxbridge**
(CDP→Juggler protocol proxy). Это нижняя половина F18-стека; агент Jev
подключается к нему единственно через `BU_CDP_URL`.

## Архитектура

```text
Jev agent ── BU_CDP_URL ──▶ Foxbridge ── Juggler ──▶ Camoufox ──▶ Web
                              ▲ CDP (loopback)
```

- Foxbridge — бинарь из `packages/foxbridge` (upstream + 4 F18-патча
  совместимости), CDP-сервер на **127.0.0.1** (bind жёстко зашит в
  `pkg/cdp/server.go`, флага нет — см. `listenAddress` ниже).
- Camoufox — из `packages/camoufox`, официальный релизный zip (плоская
  раскладка: ELF-бинарь и `.so` в корне), авто-патчится под store.
- Секретов в этом модуле нет: браузер не авторизуется.

## Опции

- `lattice.foxbridge-camoufox.enable` — включить сервис.
- `lattice.foxbridge-camoufox.package` / `camoufoxPackage` — пакеты
  (по умолчанию `pkgs.lattice.foxbridge` / `pkgs.lattice.camoufox`).
- `lattice.foxbridge-camoufox.user` / `group` — system user/group
  (по умолчанию `foxbridge`).
- `lattice.foxbridge-camoufox.listenAddress` — *контракт* loopback: Foxbridge и
  так слушает `127.0.0.1` по построению, поэтому любое не-loopback значение
  отклоняется assertion'ом. CDP наружу не публикуется (проверено f18-07).
- `lattice.foxbridge-camoufox.port` — TCP-порт CDP endpoint (по умолчанию
  `9222`).
- `lattice.foxbridge-camoufox.camoufox.headless` — headless-режим
  (по умолчанию `true`).
- `lattice.foxbridge-camoufox.camoufox.humanize` — режим `humanize` (естественные
  траектории курсора и задержки), доставляется как `CAMOU_CONFIG_1='{"humanize":true}'`
  (f18-06). По умолчанию `true`.
- `lattice.foxbridge-camoufox.camoufox.profileDir` — одноразовый профиль Firefox
  на tmpfs (по умолчанию `/run/foxbridge-camoufox/profile`).

## Примечания по окружению (f18-07 → f18-08)

- `MOZ_DISABLE_CONTENT_SANDBOX=1` обязателен: content-процессы Camoufox под
  строгим systemd-сандбоксом падают SEGV, и Juggler не отдаёт `frameId`.
- Хрупкий `LD_LIBRARY_PATH`-glob из f18-07 убран: Nix-производное
  `autopatchelf`-ится, бинарь самодостаточен.
- NixOS не имеет `/bin/bash` — модуль вызывает бинари напрямую через
  `lib.getExe`, без shebang-зависимости.
- Seccomp: в `SystemCallFilter` **нельзя** ставить `~@resources`. Firefox
  вызывает `setpriority` (syscall 141) на старте; фильтр убивает процесс
  `SIGSYS/31`, и сервис бесконечно рестартуется (на ноде: `dmesg`
  `sig=31 syscall=141`, restart counter — десятки тысяч). f18-07 PoC гонял
  un-sandboxed под root, поэтому это вскрылось только при накатке
  декларативного юнита. Оставлены `@system-service` allow-list + `~@privileged`
  и полное capability/namespace/fs-хардение. Аналогичный tradeoff-класс зафиксирован
  в `modules/verdaccio/README.md` для `MemoryDenyWriteExecute` (V8).
