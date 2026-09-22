# Jev ultrafast browser agent (F18)

Модуль предоставляет `lattice.jev-ultrafast.enable` — агент
**browser-use/jev-ultrafast** (decision loop + snapshot). Верхняя половина
F18-стека: Jev подключается к браузеру **только** через `BU_CDP_URL`, т.е. к
Foxbridge-рантайму из `lattice.foxbridge-camoufox`. Ключевое требование F18 —
**не менять** Jev policy и agent loop: используется оригинальный upstream без
локального форка.

## Архитектура

```text
Jev wrapper ─▶ jev-ultrafast ─▶ browser-harness ──BU_CDP_URL──▶ foxbridge-camoufox
                                                                (браузер)
```

- Пакет из `packages/jev-ultrafast`: upstream `jev`-script с точным pinned
  набором зависимостей (browser-harness 0.1.13, cdp-use 1.4.5, fetch-use 0.4.0,
  websockets 15.0.1 — их нет в nixpkgs, собираются как override'ы).
- Jev слушает loopback-only HTTP-инспектор (`127.0.0.1:<inspectorPort>`,
  `TYPESAFE_DEMO_PORT`); CDP-связь только на loopback.
- Секреты — только через `LoadCredential` (agenix), никогда в Nix store.

## Опции

- `lattice.jev-ultrafast.enable` — включить сервис.
- `lattice.jev-ultrafast.package` — пакет (по умолчанию `pkgs.lattice.jev-ultrafast`).
- `lattice.jev-ultrafast.user` / `group` — system user/group (по умолчанию `jev`).
- `lattice.jev-ultrafast.cdpUrl` — endpoint браузера (по умолчанию
  `http://127.0.0.1:9222`, loopback Foxbridge CDP). Не указывать публичный адрес.
- `lattice.jev-ultrafast.inspectorPort` — порт loopback-инспектора
  (по умолчанию `8766`).
- `lattice.jev-ultrafast.textModel` / `textModelBaseUrl` — текстовая модель
  (`TEXT_MODEL` / `TEXT_MODEL_BASE_URL`; по умолчанию `deepseek-chat` и
  `https://api.deepseek.com/v1`).
- `lattice.jev-ultrafast.typesafeApiKeyFile` /
  `textModelApiKeyFile` — runtime-пути к секретам (`nullOr path`). Монтируются
  через systemd `LoadCredential` и инжектируются wrapper'ом в env. При `null`
  сервис всё равно поднимается (inspector), но задачи модели не работают
  без ключа.

## Секреты

API-ключи Jev — `TYPESAFE_API_KEY` и `TEXT_MODEL_API_KEY` — читаются кодом как
обычные переменные окружения (нет `_FILE`-форм), поэтому модуль использует
паттерн `git-cache-proxy`: `LoadCredential` + маленький exec-wrapper, который
читает содержимое из `$CREDENTIALS_DIRECTORY` и экспортирует в env. В argv и в
Nix store секреты не попадают.
