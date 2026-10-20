# Foxbridge + Camoufox browser runtime (F18)

Модуль предоставляет `lattice.foxbridge-camoufox.enable` — long-running browser
runtime: **Camoufox** (антидетект-сборка Firefox) под управлением **Foxbridge**
(CDP→Juggler protocol proxy). Это нижняя половина F18-стека; агент Jev
подключается к нему единственно через `BU_CDP_URL`.

## Архитектура

```text
Jev agent ── BU_CDP_URL ──▶ Foxbridge ── Juggler ──▶ Camoufox ──▶ Web
                                                       │ X11 (virtualDisplay)
                                                       ▼
                                                      Xvfb
                              ▲ CDP (loopback)
```

- Foxbridge — бинарь из `packages/foxbridge` (upstream + F18-патчи
  совместимости и патч page WebSocket для DevTools), CDP-сервер на **127.0.0.1** (bind жёстко зашит в
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
  (по умолчанию `true`). В [профиле стека](./../../../profiles/browser-agent-stack/README.md)
  используется `false` вместе с виртуальным дисплеем.
- `lattice.foxbridge-camoufox.camoufox.virtualDisplay` — запустить headed Camoufox
  под Xvfb с программным Mesa (`LIBGL_ALWAYS_SOFTWARE=1`); по умолчанию `false`.
  Требует `headless = false`, что проверяется assertion модуля. Xvfb запускается
  в том же systemd-сервисе, не слушает TCP и завершается вместе с браузером.
  `PrivateDevices` сохраняется: физический GPU сервису не открывается.
  Seccomp допускает `setresuid` и `setuid` для проверки и сброса UID в Xvfb; User,
  NoNewPrivileges и пустой CapabilityBoundingSet по-прежнему запрещают повышение прав.
- `lattice.foxbridge-camoufox.camoufox.humanize` — режим `humanize` (естественные
  траектории курсора и задержки), доставляется как `CAMOU_CONFIG_1='{"humanize":true}'`
  (f18-06). По умолчанию `true`.

Профиль Camoufox не задаётся через `--profile`: браузер сам создаёт его по
`$HOME` (`$HOME/.cache/camoufox`). Домашний каталог — персистентный
`/var/lib/foxbridge-camoufox` (`lattice.foxbridge-camoufox.stateDir`, создаётся
tmpfiles'ом), а не tmpfs на `/run`: f18-08 валидация показала, что под полным
systemd-hardening Camoufox падает, когда его HOME лежит на tmpfs
`RuntimeDirectory`, а на обычной директории работает с той же hardening.

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
- `--profile` **неиспользуется**: Камуфокс сам создаёт профиль по `$HOME`.
  Явный `--profile` на пути `/run/...` фатален — f18-08 на ноде: `RuntimeDirectory`
  при каждом старте пересоздаётся пустым (путь исчезает), а HOME на tmpfs под
  полным hardening убивает процесс, Juggler `Browser.enable` не завершается
  (`context deadline exceeded` / `client closed`). HOME — строго персистентная
  директория `/var/lib/foxbridge-camoufox` (`stateDir`).

## Тест через Chrome DevTools

[Профиль Moli](./../../../profiles/moli/README.md) может публиковать Foxbridge
под `/foxbridge/` на общем browser-сайте с Authentik. Его
[page WebSocket patch](./../../../packages/foxbridge/devtools-page-socket.patch)
привязывает команды без `sessionId` к существующей сессии выбранной вкладки,
снимает эту обёртку с ответов и событий и использует JSON text frames.
Browser-level CDP и явные дочерние session IDs сохраняют свой контракт.
Ограничения покрытия CDP Foxbridge при этом остаются.

## Видео и WebGL

[Пакет Camoufox](./../../../packages/camoufox/README.md) добавляет FFmpeg 8,
libglvnd и Mesa в RUNPATH shared libraries, включая `libxul.so`. Путей только на
launcher недостаточно для `dlopen`. Это устраняет ошибку декодирования H.264 MP4.

Штатный Python launcher задаёт WebGL preferences; при запуске сырого бинарника
их задаёт пакет. Для программного WebGL без GPU профиль использует headed
режим под Xvfb, аналогичный
[virtual display](https://camoufox.com/python/virtual-display/) upstream.
Нативный headless остаётся доступен как отдельный режим, но WebGL в нём
не гарантирован. CSP сайта и политики origin не отключаются.

Screencast переводится [отдельным патчем](./../../../packages/foxbridge/devtools-screencast.patch):
геометрия JPEG помещается в CDP `metadata`, а подтверждения кадров переводятся
из числового CDP frame ID в строковый Juggler stream ID. Для проверки live-view
недостаточно считать полученные кадры: canvas DevTools должен иметь ненулевые
размеры и показывать изображение.
