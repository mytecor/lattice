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

- [ ] Определить набор полей метаданных, которые отдаёт endpoint (минимум — generation и source
      revision; стоит рассмотреть hostname, kernel, uptime, `system.stateVersion`, время активации).
- [ ] Определить источник коммита/ревизии, на которой работает узел (`self.rev` / `dirtyRev` из
      flake, или revision, выбранная `comin` из Radicle/GitHub remote).
- [ ] Выбрать механизм доставки: generation/commit меняются на активации без пересборки Caddy —
      обосновать, генерировать ли JSON на сборке (build-time), писать файл на активации
      (activation-time) или обслуживать через лёгкий backend.
- [ ] Расширить `profiles/app-services/config.nix` так, чтобы endpoint отдавал новые поля.
- [ ] Обновить тест `tests/app-services.nix` и (при необходимости) набор проверок statusHost.

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

- Какой источник «коммита» считается каноничным: `self.rev` (фиксирует сборку flake) против
  revision, применённой `comin` из remote (фиксирует фактически выбранный источник)? Нужно решить
  до старта или отдавать оба поля.
- Отдавать ли sensory-поля вроде uptime (меняются часто и плохо кэшируются)? — уточнить до старта.
