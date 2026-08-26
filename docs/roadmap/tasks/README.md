# Реестр задач

Третий уровень роадмапа: отдельный файл на каждую задачу. Здесь — сводная таблица. Верхнеуровневая
картина — в [VISION.md](../VISION.md), план по фичам — в [`features/`](../features/README.md).

## Как завести новую задачу

1. Скопируйте [TEMPLATE.md](./TEMPLATE.md) в `f<фича>-<номер>-<slug>.md`.
2. Заполните разделы, закройте чек-боксы по мере работы.
3. Добавьте строку в таблицу ниже и ссылку в `features/` нужной фичи.
4. Незакрытые вопросы — в [BACKLOG.md](../BACKLOG.md).

## F1. Одна железная нода

| Задача | Файл |
| ------ | ---- |
| Собрать `profiles/base` и подключить в `nodes/example` | [f1-01](./f1-01-assemble-base-profile.md) |
| Привести модель flake/не-flake к одному решению | [f1-02](./f1-02-unify-module-profile-model.md) |
| Развернуть первый узел на Intel N100 с нуля | [f1-03](./f1-03-bootstrap-intel-n100.md) |
| Убедиться, что `comin` тянет из GitHub и применяет конфиг | [f1-04](./f1-04-verify-comin-github.md) |

## F2. Секреты и идентичность

| Задача | Файл |
| ------ | ---- |
| Выбрать инструмент секретов (sops-nix/agenix) | [f2-01](./f2-01-secrets-tool.md) |
| Определить идентичность узла | [f2-02](./f2-02-node-identity.md) |
| Описать bootstrap секрета | [f2-03](./f2-03-secret-bootstrap.md) |
| Заменить секреты-заглушки | [f2-04](./f2-04-replace-dummy-secrets.md) |
| Описать ротацию и отзыв ключа | [f2-05](./f2-05-key-rotation.md) |

## F3. Reticulum поверх TCP/IP и удалённый доступ

| Задача | Файл |
| ------ | ---- |
| Добавить TCP-интерфейсы Reticulum | [f3-01](./f3-01-reticulum-tcp-interfaces.md) |
| Определить точку входа и адрес | [f3-02](./f3-02-define-entry-points.md) |
| Поднять второй узел; включить `rnsh` | [f3-03](./f3-03-second-node-rnsh.md) |
| Проверить доступ по rnsh к узлу за NAT | [f3-04](./f3-04-rnsh-nat-access.md) |

## F4. Полезная нагрузка

| Задача | Файл |
| ------ | ---- |
| Radicle-seed; `comin` на radicle-remote | [f4-01](./f4-01-radicle-seed-comin.md) |
| Профиль прикладных сервисов поверх `tcp-gateway` | [f4-02](./f4-02-app-services-profile.md) |
| Определить «общее хранилище и вычисления» | [f4-03](./f4-03-shared-storage-compute.md) |

## F5. Внешние узлы

| Задача | Файл |
| ------ | ---- |
| Вынести узел во внешний репозиторий | [f5-01](./f5-01-external-node-repo.md) |
| Описать политику доверия | [f5-02](./f5-02-trust-policy.md) |
| Довести `CONTRIBUTING.md` до инструкции | [f5-03](./f5-03-contributing-guide.md) |

## F6. Радио и mesh

| Задача | Файл |
| ------ | ---- |
| Добавить интерфейсы RNode/LoRa | [f6-01](./f6-01-rnode-lora-interfaces.md) |
| Проверить сервисы на узком канале | [f6-02](./f6-02-narrow-channel-profile.md) |
| Довести идею `MSG_BUS.md` до модуля | [f6-03](./f6-03-msgbus-module.md) |
