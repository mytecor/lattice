# Архитектура Lattice

## Архитектура сети

Сеть строится как набор равноправных нод. Каждая нода может:

- устанавливать связь с другими узлами через Reticulum
- принимать административные подключения через rnsh
- выступать как постоянный или временный участник сети

## Точки входа Reticulum

На старте узлы подключаются исходящими TCP-соединениями к двум публичным transport-узлам:
`sydney.reticulum.au:4242` и `node.reticulumnet.nl:4242`. Собственный публичный сервер не требуется.
Клиенты за NAT не открывают входящие TCP-порты. Публичные узлы маршрутизируют трафик; право
выполнять команды определяется отдельно allowlist слушателя rnsh.

Общий реестр [`profiles/networking/reticulum.nix`](./profiles/networking/reticulum.nix)
распространяется через flake/`comin`. Профиль [`rns-network`](./profiles/rns-network/README.md)
генерирует TCPClientInterface для каждого peer. AutoInterface, входящий listener, transport routing
и HTTP control plane по умолчанию выключены. Ноды получают одинаковый набор peers без ручной
настройки адреса на каждой машине. Реестр можно заменить типизированной опцией `uplinks`.

Два gateway уменьшают зависимость от единственного endpoint, но общая внешняя инфраструктура
остаётся добровольной и может иметь общие маршруты. Список — начальный bootstrap; автоматическое
обнаружение и подключение peers рассматривается после проверки поддержки в закреплённом `rns-rs`.
Механизм публичного подключения описан в
[руководстве Reticulum](https://reticulum.network/manual/gettingstartedfast.html#connect-to-the-distributed-backbone).

На homelab transport routing включён для обслуживания локального rnsh через публичные peers.
Транспорт работает как `rns`, remote shell — как отдельный пользователь `rnsh`
без sudo/root-привилегий. Identity оператора хранится на Mac отдельно от SSH/age; на ноде
разрешён её публичный hash. `/var/lib/rns` и `/var/lib/rnsh` сохраняются в `/persist`.
`--config` rnsh указывает на его собственный каталог, `--rnsconfig` — на каталог Reticulum.

## Нода

Базовая нода состоит из следующих компонентов:

- **NixOS** - операционная система узла. Используется для декларативной и воспроизводимой настройки системы
- **Reticulum** - сетевой стек для связи между узлами. Обеспечивает адресацию и маршрутизацию внутри сети Lattice
- **rnsh** - доступ по ssh поверх Reticulum. Используется для администрирования нод без прямой IP-доступности

## Структура ноды по слоям

Конфигурация каждой ноды собирается из слоев. Каждый слой отвечает только за свою часть системы и не должен дублировать ответственность других слоев.

- [hardware/](./hardware/README.md) - описывает платформу ноды: диски, загрузчик, файловые системы, драйверы, сетевые интерфейсы и параметры устройства
- [packages/](./packages/README.md) - добавляет локальные пакеты и оверлеи, если нужного пакета нет в общем репозитории Nix
- [modules/](./modules/README.md) - предоставляет переиспользуемые NixOS-модули без инфраструктурных дефолтов
- [profiles/](./profiles/README.md) - собирает повторяемые роли, сервисы и инфраструктурные дефолты сети
- [nodes/](./nodes/README.md) - связывает выбранное железо, профили, модули, секреты и файлы в итоговую конфигурацию конкретного узла

## Модель сборки

В репозитории один flake и один lock-файл. Корневой [flake.nix](./flake.nix) выбирает `nixpkgs`,
внешние зависимости и собирает `nixosConfigurations`. Локальные каталоги из `hardware/`,
`modules/`, `profiles/` и `packages/` подключаются к нему как inputs с `flake = false`; собственных
`flake.nix` и lock-файлов у этих слоёв нет.

Конкретная нода — обычный NixOS-модуль в `nodes/<name>/default.nix`. Корневой flake соединяет его
с hardware, `disko`, общими модулями и выбранными профилями. Благодаря этому изменение любого
локального слоя и конфигурации ноды фиксируется одним корневым lock-файлом, а `comin` не может
увидеть устаревший lock вложенного flake.

Зависимости модулей передаются через типизированные опции:

- пакеты `rns-server` и `rnsh` предоставляет `pkgs.lattice` из корневого overlay; опции
  `lattice.rns-server.package` и `lattice.rnsh.package` имеют тип `types.package` и могут быть
  переопределены нодой;
- значения портов берутся профилями из `profiles/networking/ports.nix`, а результирующие опции
  имеют тип `types.port` и также допускают переопределение;
- `profiles/base` содержит общий unfree-предикат для пакетов Reticulum, GitOps через `comin` и
  базовое обслуживание Nix store.

Скрытые module arguments для пакетов и портов не используются. Разные версии `nixpkgs` внутри
одного checkout сознательно не поддерживаются: вся текущая сеть обновляется атомарно через один
flake. Граница внешнего репозитория ноды будет добавлена в F5 как односторонняя зависимость внешней
ноды от экспортируемых `nixosModules` и overlay Lattice.

## Распространение source и GitOps

Репозиторий Lattice публикуется одновременно в GitHub и публичной сети Radicle под RID
`rad:z3AqC22BKQ5Gnrkw49N7PGJa91G6L`. Public identity и Git history — разные слои: visibility и
делегаты задаются подписанным Radicle identity document, а `main` остаётся обычной Git-веткой.

На ноде `radicle-node` хранит реплику как bare repository в
`/var/lib/radicle/storage/<RID без rad:>`. Сервис `lattice-comin-source-sync` читает этот локальный
путь и GitHub, а `comin` получает уже нормализованный bare repository из
`/var/lib/comin/source/repository`. Используется bare path, а не `rad://`: системный сервис не
зависит от custom helper `git-remote-rad` и не ставит `radicle-httpd` в критический путь.

Нормализатор сохраняет fast-forward историю даже после force-push upstream. При non-fast-forward
он создаёт локальный merge-коммит: первый parent — предыдущий нормализованный head, второй — новый
source commit, tree — в точности tree нового source commit. Если Radicle и GitHub расходятся,
приоритет имеет Radicle; если один head является предком другого, выбирается более новый.

Первичная конфигурация чистой ноды приходит из installer checkout или GitHub. После запуска
Radicle сервис `radicle-seed-lattice` с default policy `block` разрешает только RID Lattice со
scope `followed` и повторяет fetch, пока публичная реплика не станет доступна. До появления
storage нормализатор использует GitHub fallback. `/var/lib/radicle` и `/var/lib/comin` сохраняются
на ephemeral-root ноде.

Рабочий checkout может иметь локальный remote `publish` с двумя push URL — Radicle и GitHub.
Такая публикация не атомарна: частичный успех требует сверки и повторного push. Настройка remote и
операционная процедура описаны в [DEPLOYMENT.md](./DEPLOYMENT.md#публикация-в-radicle-и-github).

## Прикладной HTTP ingress

Локальные HTTP-сервисы, доступные клиентам ноды, следуют единому контракту:

```text
http://<service>.<node-name>.local/
```

Каждое такое имя публикуется через mDNS/Avahi и обслуживается на стандартном HTTP-порту `80`.
Единственный внешний listener на этом порту принадлежит Caddy из профиля
[`tcp-gateway`](./profiles/tcp-gateway/README.md). Caddy выбирает сервис по HTTP-заголовку `Host` и
либо формирует ответ самостоятельно, либо проксирует запрос на backend, привязанный к loopback.
Backend-порты не открываются в firewall и не являются частью клиентского API. Клиенты не должны
использовать IP ноды, ручной `Host` или внутренний порт backend вместо канонического mDNS-имени.

Маршруты публичного DNS/HTTPS, если они появятся, настраиваются отдельно и не изменяют локальный
контракт `service.node-name.local:80`.

Первый прикладной payload — JSON endpoint `status.<node>.local` из
[`profiles/app-services`](./profiles/app-services/README.md). Его обслуживает директива Caddy
`respond`, без отдельного backend-процесса и состояния; Avahi публикует service-specific hostname
в mDNS. Для будущих динамических приложений этот профиль остаётся точкой композиции, а Caddy —
единственным внешним ingress.

## LLM gateway

Runtime F7 — собственный небольшой Go HTTP proxy поверх Bifrost Core, собранный пакетом
[`packages/llm-gateway`](./packages/llm-gateway). Legacy `mxyhi/token_proxy` (input `token-proxy-src`,
package, patches и spike-тест) удалён из активной конфигурации после подтверждённого cutover;
исторические findings [f7-01](./docs/roadmap/f7-llm-gateway/f7-01-token-proxy-spike.md),
[f7-05](./docs/roadmap/f7-llm-gateway/f7-05-research-gateway-alternatives.md) и
[f7-06](./docs/roadmap/f7-llm-gateway/f7-06-go-lip-gonka-cutover.md) сохранены неизменными.

Выбранный runtime закреплён в
[f7-07](./docs/roadmap/f7-llm-gateway/f7-07-bifrost-go-proxy.md): собственный небольшой Go HTTP proxy
использует Bifrost через Go API как provider execution library. Bifrost отвечает за
provider-specific adapters, schema conversion, streaming transport и поддерживаемую им
инфраструктуру; Lattice владеет OpenAI-compatible ingress, логическими моделями, model discovery
policy и композицией маршрутов. Parallel race реализуется над отдельными вызовами Bifrost, а не
форком Bifrost, внешним proxy перед его HTTP gateway или dynamic plugin с рекурсивным вызовом.

Стабильная клиентская граница независимо от runtime — OpenAI-compatible API с отдельным gateway
credential и временным набором logical models `stupid`, `standard`. Provider credentials
и реальные model IDs существуют только внутри runtime-конфигурации gateway.

`/v1/models` принадлежит Lattice proxy и публикует только два логических имени. Входящий model
ID преобразуется в native target до вызова Bifrost, а исходное logical имя восстанавливается во
всех ответах и безопасных ошибках. Provider identity, native IDs, внутренние URLs и route selectors
не входят в клиентскую поверхность. Непубличные upstream keys подаются отдельно от client key.

Provider-конфигурация разделяет `inference_url` и опциональный `models_url`. Поэтому
`api.openbroker.gonka.gg/v1` может обслуживать inference, а каталог того же provider загружаться с
`proxy.gonka.gg/v1/models`. Если `models_url` отсутствует, gateway выводит catalog endpoint как
`${inference_url}/models`; явный URL остаётся способом использовать независимый источник.
`inference_url` задаёт полный путь до OpenAI-совместимой точки входа, включая версионный сегмент:
gateway добавляет только операцию (`/chat/completions`), не вставляя `/v1` автоматически, так что
provider может хостить API под произвольным маршрутизированным префиксом. Discovery provider-scoped:
каждый provider владеет собственным last-known-good snapshot, а точная валидация native ID против
каталога конкретного provider выполняется перед dispatch.

Маршрут строится из плоского упорядоченного `routing_rules` pipeline. Каждый rule выполняет одно
action с одной ответственностью, а compiled route отделён от внешних rules: кандидатный pool,
ranking, dispatch batches, retry/hedge schedule, semaphore limits, lease и affinity state
компилируются в отдельную структуру. Канонический порядок actions:
`map → rank → lease → affinity → race → retry → hedge → semaphore → timeout`, за которым может
следовать опциональный второй stage `map → rank → fallback`.

`map` связывает один native model ID с явным набором provider IDs и добавляет готовые
target-пары `(provider ID, native model)` в pending pool; `rank` сортирует pool по priority, а
route-creating action (`race` или `fallback`) сохраняет immutable snapshot. Провайдеры остаются
registry: rules ссылаются только на стабильные provider IDs, а access group больше не является
routing identity. `lease` поднимает текущего победителя в начало ranking, продлевается успешным
ответом и освобождается по настроенным hard failures либо после последовательных превышений
meaningful TTFT. `affinity` закрепляет stateful Responses chain (по `conversation` /
`previous_response_id`) за вернувшим её provider и fail-closed при его отказе; Chat affinity
никогда не получает. `race` запускает только top-`count` unused targets; `retry`
`scope = "next"` берёт следующие unused targets и не повторяет provider; `hedge` разрешает
следующему retry batch начаться до завершения текущих branches; `semaphore` ограничивает
суммарные/одновременные/на-провайдера upstream calls во всём request, включая fallback;
`timeout` ограничивает primary, backoff и fallback одним абсолютным deadline. Retry следующей
wave разрешён, только когда все ошибки завершившейся active wave входят в `retry.on`; итоговый
класс ошибки выбирается детерминированно. В
streaming победитель выбирается по первому meaningful content, reasoning или tool-call event;
отменённые losers не влияют на cooldown и lease. После winner, client cancellation, timeout или
исчерпания semaphore budget новые upstream calls не стартуют.

В production все настроенные Gonka providers входят в primary `map` обоих logical models,
ранжируются по priority (hyperfusion 100, gonka-proxy 50, gonka-openbroker 40, gonka-api 30, dahl
20, gonkarouter 10), а race/retry/hedge потребляют топ-2 и по одному следующему unused target при
целевом semaphore `max_calls = 4`, `max_in_flight = 3`, `max_calls_per_provider = 1` — один запрос
никогда не создаёт больше четырёх upstream calls, больше трёх concurrent calls или повторный
call к одному provider. Для `standard` второй stage даёт Hyperfusion второй catalog alias
(`gonka/deepseek-ai/DeepSeek-V4-Flash-0731`): локальное отсутствие первичного alias в каталоге
классифицируется как `model_not_found` и переводит выполнение на fallback без
лексикографической подстановки произвольной модели. Provider с retryable failure получает
cooldown и временно пропускается; если охлаждаются все ветки, gateway fail-open пробует pool
снова. Legacy `race accessGroups`, отдельный `models` registry и access-group routing удалены.

Executable spike [f7-01](./docs/roadmap/f7-llm-gateway/f7-01-token-proxy-spike.md) остаётся историческим
подтверждением требуемого поведения и источником regression tests. NixOS-модуль, безопасная сборка
runtime config и resilience contract из f7-02–f7-04 работают на собственном Bifrost-based proxy
([f7-07](./docs/roadmap/f7-llm-gateway/f7-07-bifrost-go-proxy.md)); старые package, patches, input и
runtime-specific legacy config удалены после подтверждённого прямого cutover
([f7-08](./docs/roadmap/f7-llm-gateway/f7-08-remove-token-proxy.md)).

### Контракт логических моделей

До появления дополнительных provider classes клиентская поверхность содержит два имени. Logical model registry выводится runtime из успешно скомпилированных executable plans, а не из отдельного конфигурационного списка:

| Имя | Семантика (native через `map`) |
| --- | --- |
| `stupid` | `MiniMaxAI/MiniMax-M2.7`, назначен всем provider primary stage; дешёвые и простые шаги. |
| `standard` | `deepseek-ai/DeepSeek-V4-Flash-0731` в primary stage, плюс `model_not_found`-fallback на `gonka/deepseek-ai/DeepSeek-V4-Flash-0731` (Hyperfusion); основной рабочий класс. |

Mappings можно менять без изменения клиента, если новое назначение сохраняет смысл класса,
поддерживает нужный OpenAI-compatible protocol и проходит контрактные/resilience checks. Изменение
цены, provider ID, priority или fallback внутри класса не меняет API. Перенос модели в другой класс
является изменением эксплуатационной политики и требует проверки качества, но не нового имени.

Профиль задаёт `logicalModels` этим списком, а runtime проверяет, что `/v1/models` не включает
upstream prefixes, каждый advertised ID выводится из скомпилированного плана и имеет явный
`map(native, providers)`. Неизвестное или provider-specific имя отклоняется с 404 до обращения
к upstream. Ошибки клиентской авторизации остаются 401; исчерпание retry/fallback возвращает
gateway error без credentials и без раскрытия внутреннего model ID. Native/provider данные не
попадают в client responses, SSE и безопасные ошибки.

## Стираемый root

Ноды Lattice должны поддерживать стираемый (ephemeral) root, при котором корневая файловая система
пересоздаётся при загрузке, а явно выбранное состояние хранится отдельно. Эта возможность
подключается опциональным переиспользуемым NixOS-модулем `modules/ephemeral-root`, а не встраивается
жёстко в hardware-модуль, общий профиль или конфигурацию конкретной ноды.

Границы ответственности разделены следующим образом:

- `modules/ephemeral-root` реализует пересоздание root и задаёт минимальный общий persistence;
- `nodes/<name>/disko.nix` описывает устройство, файловую систему и subvolumes конкретной ноды;
- `nodes/<name>/config.nix` расширяет persistence данными и идентичностями сервисов этой ноды;
- нода без подходящего storage или без требования к стираемому root может не подключать модуль.

Таким образом, поддержка стираемого root остаётся модульной и пригодной для разных нод, а детали
диска и набор сохраняемых данных не протекают в общий механизм.

Persistence не означает, что каждая идентичность обязана быть вечной. Нода сохраняет только те
ключи и данные, стабильность которых является частью её контракта. До включения сетевых сервисов
Reticulum identity первой ноды могла быть пересоздана при миграции. После публикации постоянного
destination соответствующий закрытый ключ становится частью сохраняемого состояния или
восстанавливается из `agenix` secret.

## Идентичность узла

Идентичность Lattice-ноды — составная. Стабильное имя `nodes/<name>` связывает конфигурацию и
набор публичных идентификаторов подсистем, но не является криптографическим ключом. Универсального
host-ключа или мастер-ключа, из которого выводятся остальные ключи, нет: каждая граница доверия
получает независимую ключевую пару и отдельный жизненный цикл.

| Идентификатор | Что подтверждает | Хранение закрытой части |
| ------------- | --------------- | ----------------------- |
| age recipient ноды | Право расшифровывать назначенные ноде secrets | Отдельный ключ в `/persist`; передаётся один раз при bootstrap |
| SSH host key | Подлинность OpenSSH-сервера | Генерируется независимо и сохраняется в `/persist` |
| Reticulum identity | Подлинность и стабильный адрес конкретного RNS-сервиса | Отдельный файл сервиса; сохраняется или доставляется через `agenix`, если destination должен быть постоянным |
| Radicle key | Подлинность и подпись Radicle-ноды | Отдельный ключ Radicle; способ доставки определяется профилем сервиса |

Одна Reticulum identity может обслуживать несколько destinations внутри одной границы доверия,
но административный `rnsh`, транспортный экземпляр и прикладные сервисы по умолчанию не делят
закрытый ключ. Reticulum Network Identity является общей учётной записью сетевого домена, если
такая схема будет выбрана в F3, и не обозначает отдельную Lattice-ноду.

SSH-ключ администратора обозначает оператора: он может быть ключом доступа и recovery recipient,
но не входит в идентичность ноды. Публичные ключи, fingerprints, Reticulum destination hashes и
Radicle DID/NID можно хранить в Git рядом с конфигурацией ноды; закрытые ключи там не хранятся.

При первичной установке передаётся только age-ключ. Остальные закрытые ключи либо генерируются на
ноде и явно добавляются в persistence, либо хранятся как `.age`-файлы и расшифровываются `agenix`
для конкретного сервиса. Ротация одного ключа не меняет имя ноды и не требует замены остальных
ключей, если они не были затронуты компрометацией.

## Секреты

Для управления секретами выбран `agenix`. Зашифрованные `.age`-файлы можно хранить рядом с
конфигурацией ноды в `nodes/<name>/secrets/` и публиковать вместе с репозиторием. Целевая схема:
корневой flake подключает модуль `agenix`, конфигурация ноды объявляет `age.secrets`, а сервисы
получают только пути `config.age.secrets.<name>.path` к расшифрованным runtime-файлам.

Открытый текст секретов не должен попадать в Nix store, Nix-выражения или Git. В частности,
runtime-секреты нельзя создавать через `pkgs.writeText`: этот механизм допустим только для явно
тестовых значений в шаблонной ноде `nodes/example`.

При bootstrap на ноду ровно один раз передаётся закрытый ключ расшифрования. Для уже доступной
машины ключ передаётся удалённо по существующему SSH-каналу до переключения системы; bootstrap не
должен требовать ввода Wi-Fi или настройки административного доступа на локальной консоли.

Первая нода `mytecor-homelab` использует отдельный age-ключ в
`/persist/var/lib/lattice/age/identity`; существующий SSH-ключ администратора является recovery
recipient. Это первый экземпляр общей модели: age-ключ не используется для SSH, Reticulum, rnsh
или Radicle. Зашифрованные файлы должны иметь как минимум получателя ноды и отдельного
recovery-получателя администратора, чтобы потеря одной ноды не блокировала ротацию секретов.

`sops-nix` в Lattice не используется. `agenix` выбран потому, что его модель age-получателей и
runtime-файлов напрямую соответствует pull-based обновлениям и уже существующим файловым опциям
модулей, например `lattice.wireless.networks.*`.

Input и NixOS-модуль `agenix` подключены; первые Wi-Fi secrets `mytecor-homelab` зашифрованы для
ключа ноды и recovery-ключа администратора и проверяются сборкой.

## Ротация и отзыв

Плановая ротация age выполняется с временным перекрытием получателей: сначала шифротексты читаются
старым и новым ключами, затем проверенный новый ключ устанавливается атомарно на `/persist`, и
только после проверки старый получатель исключается. Правила `secrets.nix` и соответствующие
шифротексты публикуются вместе; один лишь список получателей не меняет доступ к существующим файлам.
Recovery-получатель сохраняется на каждом этапе. Откат требует согласованных ключа и шифротекстов.

При компрометации новое доверие на захваченную систему не переносится: нода изолируется, её права
отзываются на доверенных участниках, а раскрытые значения секретов и затронутые сервисные ключи
заменяются. Старый age-ключ продолжает читать доступные ему версии из истории Git. Исключение
age-получателя не заменяет отзыв SSH/rnsh-доступа или изоляцию транспорта; единого механизма
криптографического исключения ноды из всех подсистем сейчас нет.

Команды, проверки, порядок публикации через `comin`, особенности Wi-Fi-only homelab и границы
ещё не развёрнутых сервисов описаны в [KEY_MANAGEMENT.md](./KEY_MANAGEMENT.md).
