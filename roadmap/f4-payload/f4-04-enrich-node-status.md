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

- [ ] `curl http://status.<node>.local/` возвращает валидный JSON, содержащий поколение системы и
      актуальный коммит/ревизию конфигурации узла.
- [ ] Значения соответствуют реальному состоянию узла, а не константе (проверено на
      `mytecor-homelab` после активации нового поколения).
- [ ] Документация (`profiles/app-services/README.md`) описывает новые поля endpoint.

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
`file_server`. `tests/app-services.nix` переведён на новые контракты (file_server вместо respond,
поля из activation-скрипта); добавлен runtime smoke-тест `tests/node-status.nix`. `nix flake check
--all-systems --no-build` проходит. Осталось live-подтверждение на `mytecor-homelab` (критерий
«значения реального состояния»).
