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
| Создать и развернуть `mytecor-homelab` на Intel N100 | [f1-03](./f1-03-bootstrap-intel-n100.md) |
| Убедиться, что `comin` тянет из GitHub и применяет конфиг | [f1-04](./f1-04-verify-comin-github.md) |

## F2. Секреты и идентичность

| Задача | Файл |
| ------ | ---- |
| Подключить выбранный `agenix` | [f2-01](./f2-01-secrets-tool.md) |
| Определить идентичность узла | [f2-02](./f2-02-node-identity.md) |
| Описать bootstrap секрета | [f2-03](./f2-03-secret-bootstrap.md) |
| Заменить секреты-заглушки | [f2-04](./f2-04-replace-dummy-secrets.md) |
| Описать ротацию и отзыв ключа | [f2-05](./f2-05-key-rotation.md) |

## F3. Reticulum поверх TCP/IP и удалённый доступ

| Задача | Файл |
| ------ | ---- |
| Добавить TCP-интерфейсы Reticulum | [f3-01](./f3-01-reticulum-tcp-interfaces.md) |
| Определить точку входа и адрес | [f3-02](./f3-02-define-entry-points.md) |
| Вторая постоянная NixOS-нода — отменена | [f3-03](./f3-03-second-node-rnsh.md) |
| Проверить доступ по rnsh к узлу за NAT | [f3-04](./f3-04-rnsh-nat-access.md) |

## F4. Полезная нагрузка

| Задача | Файл |
| ------ | ---- |
| Radicle source bootstrap; `comin` на radicle-remote | [f4-01](./f4-01-radicle-seed-comin.md) |
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

## F7. LLM gateway

| Задача | Файл |
| ------ | ---- |
| Проверить `mxyhi/token_proxy` executable spike | [f7-01](./f7-01-token-proxy-spike.md) |
| Собрать декларативный gateway service с секретами из agenix | [f7-02](./f7-02-declarative-gateway-service.md) |
| Зафиксировать контракт логических моделей | [f7-03](./f7-03-logical-model-contract.md) |
| Проверить routing, отказоустойчивость и streaming | [f7-04](./f7-04-routing-resilience-tests.md) |
| Исследовать альтернативы `token_proxy` | [f7-05](./f7-05-research-gateway-alternatives.md) |
| Проверить Go LIP на Gonka и выполнить прямой cutover | [f7-06](./f7-06-go-lip-gonka-cutover.md) |

## F8. Интерактивный Pi runtime

| Задача | Файл |
| ------ | ---- |
| Упаковать и закрепить Pi | [f8-01](./f8-01-package-pi.md) |
| Подключить Pi к gateway без provider-specific конфигурации | [f8-02](./f8-02-pi-gateway-config.md) |
| Собрать воспроизводимый профиль tools | [f8-03](./f8-03-reproducible-tool-profile.md) |
| Провести интерактивную acceptance-проверку | [f8-04](./f8-04-interactive-acceptance.md) |
| Зафиксировать общий TUI/RPC контракт Pi | [f8-05](./f8-05-pi-rpc-contract.md) |

## F9. Cache и artifact plane

| Задача | Файл |
| ------ | ---- |
| Развернуть Git cache proxy | [f9-01](./f9-01-git-cache-proxy.md) |
| Ограничить доступ proxy по репозиториям | [f9-02](./f9-02-git-repository-access.md) |
| Развернуть Verdaccio | [f9-03](./f9-03-verdaccio.md) |
| Развернуть и проверить Attic | [f9-04](./f9-04-attic.md) |
| Определить контракт artifacts в S3 | [f9-05](./f9-05-artifact-contract.md) |
| Доказать disposable-семантику caches | [f9-06](./f9-06-cache-loss-drill.md) |

## F10. Disposable worker

| Задача | Файл |
| ------ | ---- |
| Определить версионируемую task specification | [f10-01](./f10-01-task-specification.md) |
| Выбрать и реализовать границу изоляции worker | [f10-02](./f10-02-worker-isolation.md) |
| Собрать полный жизненный цикл worker | [f10-03](./f10-03-worker-lifecycle.md) |
| Запускать task через Pi RPC | [f10-04](./f10-04-pi-rpc-runner.md) |
| Выдавать worker минимальные временные credentials | [f10-05](./f10-05-worker-credentials.md) |
| Провести acceptance-тест уничтожения и восстановления | [f10-06](./f10-06-disposability-acceptance.md) |

## F11. Controller

| Задача | Файл |
| ------ | ---- |
| Определить модель control-plane state | [f11-01](./f11-01-control-state-model.md) |
| Реализовать очередь, leases и worker registry | [f11-02](./f11-02-queue-leases-registry.md) |
| Подключить provisioner disposable workers | [f11-03](./f11-03-worker-provisioner.md) |
| Сделать выполнение идемпотентным и восстанавливаемым | [f11-04](./f11-04-idempotent-recovery.md) |
| Зафиксировать публикацию commit/result/artifacts | [f11-05](./f11-05-result-publication.md) |
| Провести end-to-end recovery drill | [f11-06](./f11-06-end-to-end-recovery.md) |
