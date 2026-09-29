# Прикладные сервисы

Профиль подключает первый прикладной payload узла и импортирует
[`tcp-gateway`](../tcp-gateway/README.md). Endpoint `lattice-node-status` обслуживается напрямую
Go-сервисом из [`packages/node-status`](../../packages/node-status/README.md). Он слушает только
`127.0.0.1:9217`; Caddy публикует API через общий ingress, а Prometheus скрейпит `/metrics`
на loopback. Внутренний порт не открывается в firewall.

Для ноды `<node>` endpoint имеет отдельный mDNS-адрес:

```text
http://status.<node>.local/
```

Avahi публикует service-specific address alias, поэтому дополнительная DNS-запись, IP адрес и
ручной заголовок `Host` клиенту не нужны. Проверка с машины в той же LAN:

```sh
curl --fail http://status.mytecor-homelab.local/
```

## web-клиент ACP (f13-01)

Профиль также раздаёт статический SPA `acp-web` ([`packages/acp-web`](../../packages/acp-web/package.nix))
на адресе `acp-ui.<node>.local` через Caddy `file_server` (SPA-fallback на `index.html`).
Публикуется service-specific mDNS alias, как и у статус-сайта:

```text
http://acp-ui.<node>.local/
```

### Mesh-доступ (f13-01, 2026-09-20)

Как и status, `acp-ui` выкладывается дополнительно и на mesh-домене (`tcp-gateway.meshDomain`),
обслуживая тот же статический SPA, что и на LAN. При наличии Cloudflare-токена (`acme_dns`)
это HTTPS, с тем же контентом, что у LAN-контракта:

```text
https://acp-ui.<meshDomain>/
```

Между LAN- и mesh-сайтами используется один и тот же `extraConfig` — внешний адрес не несёт
отдельной копии конфигурации сайта. Клиент (см. `patch-main-ts.mjs`) сам выводит ACP ingress
из собственного hostname и выбирает `wss://`/`ws://` по схеме страницы, так что mesh-UI
подключается к `wss://acp.<meshDomain>/` без ручной настройки и без mixed-content блокировки.

## HTTP API и текущий статус

`GET /` сохраняет исходный контракт runtime-метаданных и добавляет объект `system` с текущими
CPU, load average, памятью, корневой файловой системой, uptime и состояниями контролируемых
systemd-юнитов. `GET /healthz` — дешёвая liveness-проверка, `GET /metrics` — Prometheus text
exposition. Caddy не публикует `/metrics` наружу: endpoint доступен только Prometheus по loopback.

```json
{
  "node": "mytecor-homelab",
  "service": "lattice-node-status",
  "generation": 42,
  "commit": "d28b1987d705c6684cdd6c745deae86cb452fc5c",
  "kernel": "6.12.10",
  "stateVersion": "26.05",
  "activatedAt": 1758042700,
  "system": {
    "uptimeSeconds": 86400,
    "load1": 0.18,
    "load5": 0.21,
    "load15": 0.19,
    "cpuUtilization": 0.07,
    "memoryTotalBytes": 16777216000,
    "memoryAvailableBytes": 10485760000,
    "memoryUsedBytes": 6291456000,
    "rootTotalBytes": 499963174912,
    "rootAvailableBytes": 402653184000,
    "services": { "caddy.service": "active" }
  }
}
```

Поля:

| Поле | Тип | Источник | Назначение |
| --- | --- | --- | --- |
| `node` | string | `hostname` при старте сервиса | Имя узла. |
| `service` | string | константа | Имя сервиса — `lattice-node-status`. |
| `generation` | number \| null | первый уровень `readlink /run/current-system` → `system-N-link` | Текущее NixOS поколение; по нему можно откатиться. `null`, если поколение не читается. |
| `commit` | string \| null | `refs/lattice/source` в `/var/lib/comin/source/repository` | Коммит, фактически выбранный `comin` **source sync** (до нормализации) — то значение, которое [comin-source-sync](../gitops/comin-source-sync.sh) решил применить. Это канонический ответ на вопрос «на каком коммите работает узел». `null` на свежей ноде до первого `comin`-цикла. |
| `kernel` | string | `uname -r` | Версия ядра. |
| `stateVersion` | string | `system.stateVersion` (build-time) | Версия состояния конфигурации. |
| `activatedAt` | number | mtime `/run/current-system` | Unix-время последней активации поколения. |
| `system` | object | `/proc`, `/sys`, `statfs`, systemd | Текущие показатели системы и состояния сервисов. |

Исторические ряды не хранятся самим сервисом: их собирает Prometheus. Поэтому перезапуск
`node-status` не теряет историю дашборда.

## Runtime-метаданные

`self.rev` для comin-сборок не является каноническим: он пуст на грязном дереве и не отражает
фактически применённый источник. `commit` поэтому читается из runtime-фактов
(`refs/lattice/source`) — это значение, которое реально выбрал `comin-source-sync`, соответствуя
критерию «значения соответствуют реальному состоянию узла, а не константе».

Go-сервис читает generation, commit и время активации при каждом запросе: metadata обновляется даже
если очередной `comin` switch не потребовал перезапуска самого юнита.

## Метрики

`/metrics` экспортирует CPU time/utilization, load average, RAM/swap, заполнение root filesystem,
I/O физических block devices, сетевой трафик/errors/drops, thermal zones, uptime, длительность и
ошибки сбора. Для ключевых systemd-юнитов доступны `node_status_systemd_unit_state` и
`node_status_systemd_unit_restarts_total`.

## Примечания

- Профиль открывает только стандартные порты gateway; `9217` остаётся loopback-only.
- Юнит использует `DynamicUser`, `NoNewPrivileges`, `ProtectSystem=strict` и пустой capability set.
