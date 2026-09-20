# Прикладные сервисы

Профиль подключает первый прикладной payload узла и импортирует
[`tcp-gateway`](../tcp-gateway/README.md). Endpoint `lattice-node-status` обслуживается напрямую
директивой Caddy `file_server` над JSON-файлом, сгенерированным на активации: отдельный
backend-процесс, внутренний порт и persistent state ему не нужны.

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

## Формат ответа (f4-04)

Endpoint отдаёт JSON с runtime-метаинформацией узла, сгенерированный на каждой активации
активационным скриптом `lattice-node-status` ([`status-write.sh`](./status-write.sh)) в
`/run/lattice-node-status.json`. Caddy отдаёт файл как `application/json` без какого-либо backend
процесса.

```json
{
  "node": "mytecor-homelab",
  "service": "lattice-node-status",
  "generation": 42,
  "commit": "d28b1987d705c6684cdd6c745deae86cb452fc5c",
  "kernel": "6.12.10",
  "stateVersion": "26.05",
  "activatedAt": 1758042700
}
```

Поля:

| Поле | Тип | Источник | Назначение |
| --- | --- | --- | --- |
| `node` | string | `hostname` (при активации) | Имя узла. |
| `service` | string | константа | Имя сервиса — `lattice-node-status`. |
| `generation` | number \| null | первый уровень `readlink /run/current-system` → `system-N-link` | Текущее NixOS поколение; по нему можно откатиться. `null`, если поколение не читается. |
| `commit` | string \| null | `refs/lattice/source` в `/var/lib/comin/source/repository` | Коммит, фактически выбранный `comin` **source sync** (до нормализации) — то значение, которое [comin-source-sync](../gitops/comin-source-sync.sh) решил применить. Это канонический ответ на вопрос «на каком коммите работает узел». `null` на свежей ноде до первого `comin`-цикла. |
| `kernel` | string | `uname -r` | Версия ядра. |
| `stateVersion` | string | `system.stateVersion` (build-time) | Версия состояния конфигурации. |
| `activatedAt` | number | `date +%s` при активации | Unix-время последней активации (привязка к «когда включили это поколение»). |

Sensory-поля вроде uptime намеренно не включены: они меняются часто и плохо кэшируются; endpoint
остаётся детерминированным между активациями.

## Почему activation-time, а не build-time

`self.rev` для comin-сборок не является каноническим: он пуст на грязном дереве и не отражает
фактически применённый источник. `commit` поэтому читается из runtime-фактов
(`refs/lattice/source`) — это значение, которое реально выбрал `comin-source-sync`, соответствуя
критерию «значения соответствуют реальному состоянию узла, а не константе».

Файл пишется транзакционно (tmp + `mv`), так что Caddy никогда не отдаёт частично записанный JSON.

## Примечания

- Профиль открывает только стандартные порты gateway; отдельного backend-порта нет.
- Активационный скрипт работает без отдельного systemd-юнита; Caddy читает файл только по запросу.
