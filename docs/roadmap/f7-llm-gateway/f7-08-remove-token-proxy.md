# Удалить `token_proxy` целиком из активной конфигурации

Фича: [F7 — LLM gateway](./README.md). Завершает runtime-часть cutover из
[f7-07](./f7-07-bifrost-go-proxy.md): переводит модуль `llm-gateway` на bifrost-only и убирает
последние артефакты `mxyhi/token_proxy`. Исторические task findings (`f7-01`, `f7-05`, `f7-06`)
сохраняются неизменными.

## Контекст

После перевода homelab на собственный Go proxy ([f7-07](./f7-07-bifrost-go-proxy.md)) legacy
runtime `token-proxy` больше не используется. Он остался как:

- пакет `pkgs.lattice.token-proxy` и input `token-proxy-src` в корневом `flake.nix`;
- executable spike-test `checks.x86_64-linux.token-proxy-spike`, который взял на себя упавший CI;
- legacy-ветка в модуле `modules/llm-gateway` (опция `runtime = "token-proxy"`, схема `upstreams`,
  опции `routing`/`logicalModels`/`modelListPrefix`/`sameUpstreamRetryCount`/
  `retryableFailureCooldownSeconds`, default пакета и legacy-ветки конфигурации и assertions);
- упоминания в документации (`packages/README.md`, `nodes/mytecor-homelab/README.md`).

Homelab и все тесты модуля уже используют `runtime = "bifrost"` с явным
`package = pkgs.lattice.llm-gateway`. Модуль при этом держит default
`package = pkgs.lattice.token-proxy` и legacy-ветку только для «безопасного cutover» — после
удаления пакета из overlay этот default сломан, поэтому legacy-ветку нужно вычистить, а не
оставлять.

## Что сделать

### 1. flake.nix и flake.lock

- [x] Удалить input `token-proxy-src`.
- [x] Удалить пакет `token-proxy = final.callPackage ./packages/token-proxy/package.nix` из overlay.
- [x] Убрать `token-proxy` из `inherit (pkgs.lattice)` в `packages`.
- [x] Удалить `checks.x86_64-linux.token-proxy-spike`.
- [x] Обновить `flake.lock` (`nix flake lock`), чтобы из inputs исчез `token-proxy-src`.

### 2. Модуль `modules/llm-gateway` → bifrost-only

- [x] `options.nix`: сделать bifrost единственным runtime — убрать `runtime`-enum с `"token-proxy"`,
  default `package` с `pkgs.lattice.token-proxy`, legacy-опции (`upstreams`, `routing`,
  `logicalModels`, `modelListPrefix`, `sameUpstreamRetryCount`,
  `retryableFailureCooldownSeconds`).
- [x] `config.nix`: удалить все legacy-ветки — `legacyPublicConfig`, `publicUpstream`,
  `legacyCredentials`, `appendLegacyCredential`, `useBifrost`-переключения, legacy-assertions,
  branch по `runtimeConfigFile` (`config.jsonc`). Оставить только bifrost-путь.
- [x] `README.md`: убрать упоминание legacy runtime token-proxy; оставить bifrost-only описание.

### 3. Профиль, тесты и homelab

- [x] `profiles/llm-gateway/config.nix`: убрать `runtime`/`package` default и legacy-опции
  (`modelListPrefix`, `sameUpstreamRetryCount`, `retryableFailureCooldownSeconds`, `routing`),
  оставить bifrost-опции.
- [x] `tests/llm-gateway.nix` (legacy evaluation-тест): удалён — полностью дублировался
  `llm-gateway-bifrost.nix`; единственный, кто задавал `runtime = "token-proxy"`.
- [x] `nodes/mytecor-homelab/config.nix`: убрать явные `runtime = "bifrost"` и
  `package = pkgs.lattice.llm-gateway` (стали умолчанием).
- [x] `nodes/mytecor-homelab/README.md`: обновить строку про «`token-proxy` привязан к
  `127.0.0.1:9208`» на текущий Go proxy.

### 4. Документация

- [x] `packages/README.md`: заменить раздел про `token-proxy` на актуальный про `llm-gateway`.
- [x] `README.md`: убрать упоминание `mxyhi/token_proxy` в контракте F7.
- [x] `docs/roadmap/f7-llm-gateway/f7-07-bifrost-go-proxy.md`: отметить пункт об удалении как выполненный.
- [x] Обновить реестр задач `docs/roadmap/tasks/README.md`: добавить строку `f7-08`.

## Критерий готовности

- `token_proxy`/`token-proxy`/`token_proxy_src` отсутствуют в active system closure/config, in
  `flake.nix`, `flake.lock`, модуле `modules/llm-gateway`, профиле, homelab-конфиге и активной
  документации.
- `nix flake check --no-build` (и по возможности полный `nix flake check`) проходит; все три
  gateway-теста (`llm-gateway`, `llm-gateway-bifrost`, `llm-gateway-service`) зелёные.
- `nix build .#checks.x86_64-linux.llm-gateway-*` собирается без ссылки на удалённый пакет.
- Bifrost runtime остаётся единственным и homelab-конфиг проходит evaluation.

## Статус

Начато 2026-09-07. Из `flake.nix` удалены input `token-proxy-src`, пакет
`pkgs.lattice.token-proxy`, запись в `packages` и check `token-proxy-spike`; удалены файлы
`packages/token-proxy/` и `tests/token-proxy-spike.{nix,py}`; обновлён `packages/README.md`.

Завершено 2026-09-07: обновлён `flake.lock` (input исчез); модуль `modules/llm-gateway` переведён
на bifrost-only — из `options.nix` убраны `runtime`-enum, default `package`, `upstreams`,
`routing`, `logicalModels`, `modelListPrefix`, `sameUpstreamRetryCount`,
`retryableFailureCooldownSeconds`; в `config.nix` удалены все legacy-ветки (остался только
bifrost-путь с `config.json`); профиль оставляет `runtime`/`package` умолчаниям модуля;
`tests/llm-gateway.nix` удалён как дубликат `llm-gateway-bifrost.nix`; homelab-конфиг и README
приведены к Go proxy; обновлены root `README.md`, `ARCHITECTURE.md`, `docs/roadmap/README.md`,
фича F7, модуль/profile README.

Проверено `nix flake check --no-build` (модульные checks) и evaluation checks
`llm-gateway-bifrost`, `llm-gateway-service`, `mytecor-homelab`, `app-services`,
`ephemeral-root-module` — все зелёные. `token_proxy`/`token-proxy`/`token_proxy_src` отсутствуют
в активной конфигурации, модуле, профиле, homelab и активной документации; исторические findings
`f7-01`/`f7-05`/`f7-06` сохранены неизменными.
