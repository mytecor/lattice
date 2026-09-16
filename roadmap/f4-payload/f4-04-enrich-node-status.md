# Обогатить JSON endpoint `status.<node>.local` метаинформацией системы

Фича: [F4 — полезная нагрузка](./README.md). Продолжение
[f4-02](./f4-02-app-services-profile.md).

## Контекст

`profiles/app-services` публикует `http://status.<node>.local/` через Caddy `respond` со статическим
JSON `{"node":<hostName>,"service":"lattice-node-status"}`. Это полезно как liveness, но не отвечает
на ключевой вопрос эксплуатации: *что именно сейчас крутится на узле* и *откуда оно взялось*.
Нужно, чтобы endpoint отдавал поколение системы (NixOS generation), коммит/ревизию конфигурации,
на которой работает узел, и другую базовую метаинформацию — тогда по одному URL можно понять,
деплой догнал последний коммит, и на каком поколении можно откатиться.

## Что сделать

- [x] Определить набор полей метаданных, которые отдаёт endpoint (минимум — generation и source
      revision; стоит рассмотреть hostname, kernel, uptime, `system.stateVersion`, время активации).
      → **Решено**: `node`, `service`, `generation`, `commit`, `kernel`, `stateVersion`, `activatedAt`.
      Sensory-поля (uptime) намеренно исключены.
- [x] Определить источник коммита/ревизии, на которой работает узел (`self.rev` / `dirtyRev` из
      flake, или revision, выбранная `comin` из Radicle/GitHub remote).
      → **Канонический — runtime `refs/lattice/source`** из `/var/lib/comin/source/repository`
      (значение, которое выбрал `comin-source-sync` до нормализации). `self.rev` отклонён: он
      пуст на грязном дереве и не отражает фактически применённый источник.
- [x] Выбрать механизм доставки: generation/commit меняются на активации без пересборки Caddy —
      обосновать, генерировать ли JSON на сборке (build-time), писать файл на активации
      (activation-time) или обслуживать через лёгкий backend.
      → **activation-time**: активационный скрипт `lattice-node-status` пишет
      `/run/lattice-node-status.json` (tmp + `mv`, транзакционно); Caddy отдаёт через `file_server`
      без отдельного backend (контракт f4-02 сохранён — без внутреннего порта).
- [x] Расширить `profiles/app-services/config.nix` так, чтобы endpoint отдавал новые поля.
- [x] Обновить тест `tests/app-services.nix` и (при необходимости) набор проверок statusHost;
      добавлен runtime smoke-тест `tests/node-status.nix` (сценарии: generation из symlink,
      commit из refs, commit=null на свежей ноде, отсутствие current-system → generation=null).

## Критерий готовности (Definition of Done)

- [x] `curl http://status.<node>.local/` возвращает валидный JSON, содержащий поколение системы и
      актуальный коммит/ревизию конфигурации узла.
      → **Подтверждено 2026-09-16**: `curl --fail http://status.mytecor-homelab.local/` с машины
      в LAN возвращает HTTP 200 и JSON: `generation=15`, `commit=5638323...`, а также kernel,
      stateVersion, activatedAt.
- [x] Значения соответствуют реальному состоянию узла, а не константе (проверено на
      `mytecor-homelab` после активации нового поколения).
      → `commit` = фактически применённый `refs/lattice/source`; `generation` — текущее NixOS
      поколение из `/nix/var/nix/profiles/system`. Подтверждено live.
- [x] Документация (`profiles/app-services/README.md`) описывает новые поля endpoint.

## Затрагиваемые файлы / слои

- `profiles/app-services/config.nix`, `profiles/app-services/README.md`
- `tests/app-services.nix`
- `flake.nix` (источник revision, если потребуется), `modules/`, `nodes/`

## Открытые вопросы

- ~~Какой источник «коммита» считается каноничным~~ — решено: runtime `refs/lattice/source`
  (`comin-source-sync`), а не `self.rev`. См. выше.
- ~~Отдавать ли sensory-поля вроде uptime~~ — решено: не отдавать.

## Прогресс

2026-09-16: реализована декларативная часть. `profiles/app-services/config.nix` пишет
`/run/lattice-node-status.json` активационным скриптом `lattice-node-status` из
[`status-write.sh`](../../profiles/app-services/status-write.sh); Caddy отдаёт его через
`file_server` (root `/run` + rewrite на файл, без 308-redirect). `tests/app-services.nix`
переведён на новые контракты; добавлен runtime smoke-тест `tests/node-status.nix`. `nix flake
check --all-systems --no-build` проходит.

**2026-09-16 live: задача закрыта.** Применена на `mytecor-homelab` через comin (коммит
`5638323`). `curl --fail http://status.mytecor-homelab.local/` с машины в LAN возвращает HTTP 200
и актуальный JSON (`generation=15`, `commit=5638323...`, kernel, stateVersion, activatedAt).
Значения реальные, а не константа.

Live-находки, исправленные по ходу:

- `writeShellApplication` НЕ кладёт runtimeInputs в PATH исполнения — activation-среда NixOS
  вызывает скрипты с минимальным PATH, и первая же внешняя команда (git/jq/hostname) падала 127,
  валя comin-switch. Решение: скрипт собирается через `replaceVarsWith`, вшивая полные
  store-пути `bash`/`git`/`jq`/`hostname`; остальное (coreutils) есть в PATH активации.
- `/run/current-system` на этой ноде указывает прямо на store-путь (без `system-N-link`),
  поэтому номер поколения читается из `/nix/var/nix/profiles/system` (readlink → `system-N-link`),
  с fallback на `/run/current-system`.
- `substituteAll` удалён из текущего nixpkgs — используется `replaceVarsWith`.
- `file_server` с root-файлом на `/` даёт 308-redirect-loop (root-файл трактуется как директория) —
  root указывает на `/run`, а `rewrite * /lattice-node-status.json` отдаёт файл по любому пути.
