# Radicle source bootstrap; `comin` на radicle-remote

Фича: [F4 — полезная нагрузка](../features/f4-payload.md).

## Контекст

Цель F4: конфигурация сети распространяется без обязательного GitHub. На текущем этапе Radicle
работает на основной NixOS-ноде; второй постоянной NixOS-ноды в плане нет. Нужно сделать Radicle
рабочим source origin для `comin` и будущих disposable workers, сохранив GitHub как независимый
remote. Отдельно решается первичная загрузка: чистая нода должна получить конфигурацию до того,
как сможет обновляться из локального Radicle storage.

## Что сделать

- [x] Декларативно поднять Radicle service на `mytecor-homelab` и закрепить нужные repositories.
- [x] Проверить clone/fetch нужного commit через Radicle с другого чистого клиента.
- [x] Переключить `comin` на radicle-remote как основной источник, GitHub — зеркало.
- [x] Решить проблему первичной загрузки radicle-хранилища на новый узел.

## Критерий готовности

- [ ] `mytecor-homelab` применяет конфиг из Radicle при недоступном GitHub.
- [ ] Новый узел получает radicle-хранилище и сам обновляется из него.

## Текущий прогресс

2026-09-05 декларативная часть применена на `mytecor-homelab`:

- подключены `radicle-node` и loopback-only `radicle-httpd` с отдельным ключом из `agenix`;
- `/var/lib/radicle` добавлен в persistent storage;
- selective policy оставляет default `block`, а `radicle-seed-lattice` идемпотентно разрешает и
  получает RID Lattice со scope `followed`;
- `comin` сначала читает каноническую `main` из локального bare storage Radicle, затем использует
  GitHub как fallback;
- общий RID и путь storage вынесены в `profiles/radicle/repositories.nix`.

Репозиторий переведён из private в public identity revision
`d28b1987d705c6684cdd6c745deae86cb452fc5c`. Коммит `2c70a7f` реплицирован на Iris, Rosa и
Heptapod; одноразовый чистый клиент Radicle 1.10.2 успешно клонировал его с Rosa. После общего
push коммит `8533477` доступен в GitHub и на Rosa. На homelab активны `radicle-node`,
`radicle-httpd` и `radicle-seed-lattice`; сервисная identity имеет DID
`did:key:z6Mkvw9xTo5bXFHvQvR6csSC49MqNiK8oemNj7fxkJp5thJJ`, policy Lattice — `allow/followed`.

`comin` применил `2c70a7f` из `origin/main`, затем выбрал и успешно вычислил `8533477` из
`radicle/main`. Второй switch не потребовался, потому что пустой acceptance commit даёт тот же
system closure. Это подтверждает Radicle fetch и приоритет remote, но не строгий сценарий отказа
GitHub.

`nix flake check path:. --no-build --all-systems` проходит; полный Linux system closure успешно
собран самой homelab-нодой при применении `2c70a7f`. До закрытия критериев остаются отдельный
drill с недоступным GitHub и полный bootstrap новой NixOS-ноды с последующим self-update.

## Затрагиваемые файлы / слои

- `profiles/radicle`, `profiles/gitops`
- `nodes/`

## Открытые вопросы

Архитектурных вопросов нет. Незакрытые runtime-критерии перечислены выше.
