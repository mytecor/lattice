# f18-11. Интеграционный тест полного пути на ноде

## Контекст

Нужен автоматический интеграционный тест, который на развёрнутой ноде проходит весь стек
`jev-ultrafast → browser-harness → Foxbridge → Camoufox` и подтверждает рабочее состояние
декларативной конфигурации.

## Что сделать

- [x] Контракт-тест `tests/f18-browser-stack.nix` (по [tests/README.md](../../tests/README.md)),
      зарегистрирован в `checks.x86_64-linux.f18-browser-stack` (eval-фаза проходит локально,
      сборка — в CI на x86_64-linux, как и остальные Linux-тесты репозитория). Проверяет
      **wiring-инварианты** стека, которые живое дебаггирование f18-08/f18-09 превратило в
      жёсткие регрессии:
      - `jev-ultrafast` `Requires=`+`After=foxbridge-camoufox.service` (lifecycle, f18-09 S3);
      - `ExecStartPre` Jev — сгенерированный CDP-пробер `jev-wait-cdp` (readiness-контракт,
        а не просто `After=`);
      - seccomp Foxbridge: НЕТ `~@resources` (иначе SIGSYS/31 crash-loop на setpriority,
        f18-08 207b0d8), есть `@system-service` + `~@privileged`;
      - браузерный home персистентный `/var/lib/foxbridge-camoufox` (не tmpfs `/run`,
        f18-08 06c6d4e), без `RuntimeDirectory`-tmpfs;
      - CDP loopback-only: модуль имеет assertion на listenAddress, порты не в firewall;
      - секреты только через `LoadCredential` из agenix-пути (не в Nix store),
        `ExecStart` Jev — обёртка `jev-ultrafast-exec` (не argv);
      - `ExecStart` Foxbridge — инлайн `--port` + `--binary` (Nix-бинарь).
- [x] Проба CDP извне хоста (negative test): `ss -tlnp` на ноде показал
      `127.0.0.1:9222` (foxbridge) и `127.0.0.1:8766` (jev-inspector) — слушают только loopback;
      на LAN-интерфейсах (enp3s0/wlp2s0/ygg0) CDP недоступен.
- [x] Полный путь на живой ноде после `nixos-rebuild switch` — подтверждён повторным прогоном
      f18-04 smoke (2026-09-23) поверх systemd-стека: open → snapshot → click → fill →
      DOM-изменение → `SUBMITTED:first` → `DONE`; freshness guard (StalePage) тоже подтверждён.
- [ ] Полный путь с реальной web-задачей Jev (flights/examples) — **ключевое: 2026-09-23
      принято решение не секрет, а gateway.** Homelab LLM-gateway на loopback
      (`127.0.0.1:9208`, `api=openai-completions`, модель `standard` → DeepSeek-V4-Flash-0731,
      без client-auth) отвечает на `POST /v1/chat/completions` HTTP 200. Jev можно переключить
      на него через `lattice.jev-ultrafast.textModelBaseUrl = "http://127.0.0.1:9208/v1"` +
      `textModel = "standard"` — новых секретов не нужно. Прогон — в f18-12 п.7.

## Критерий готовности (Definition of Done)

- [x] Контракт-тест `f18-browser-stack.nix` проходит eval на CI-конфигурации
      (`nix flake check`), ловит регрессии полного пути на уровне wiring.
- [x] Полный путь (smoke) подтверждён на живой ноде без ручных шагов после применения
      `main` через comin.
- [ ] Полный путь с реальной web-задачей Jev (flights/examples) подтверждён на ноде
      (через LLM-gateway, см. выше) — один прогон после `nixos-rebuild switch` с
      переключённым textModelBaseUrl.

## Затрагиваемые файлы / слои

- `tests/f18-browser-stack.nix`, `tests/default.nix` (регистрация чека).
- Smoke из f18-04 (`/root/f18-poc/jev-f18-smoke.py`) переиспользован как ядро проверки
  на живой ноде.
