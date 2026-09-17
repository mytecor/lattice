# Доступ к сервисам homelab с поддоменов myt.su через Yggdrasil

Фича: [F4 — полезная нагрузка](./README.md). Продолжение
[f4-02](./f4-02-app-services-profile.md).

## Контекст

Сейчас прикладные сервисы homelab доступны только в LAN по каноническому контракту
`http://<service>.<node>.local:80` (Caddy из `profiles/tcp-gateway`, публикация через mDNS/Avahi).
Чтобы открыть те же сервисы наружу без белого IP и проброса портов, добавляем
[Yggdrasil](https://yggdrasil-network.github.io/) —
self-organizing IPv6 mesh-оверлей, который даёт ноде стабильный криптографический адрес в
`200::/7`, достижимый через публичные peers даже за NAT. Из этого адреса выпускаем публичные
поддомены `myt.su`, каждый из которых мапится на соответствующий сервис ноды.

Yggdrasil здесь — именно транспорт внешнего доступа к сервисам F4, а не дополнительный интерфейс
Reticulum (это отдельный вопрос в [BACKLOG](../BACKLOG.md)). Ключи и идентичность ноды храним
в agenix, как остальные секреты ([f2-02](../f2-secrets-identity/f2-02-node-identity.md)).

Зависит от [F2](../f2-secrets-identity/README.md) (agenix) и от ingress из
[f4-02](./f4-02-app-services-profile.md). Соответствует [вехе 4](../../ROADMAP.md#f4-полезная-нагрузка).

## Что сделать

- [x] Включить Yggdrasil на `mytecor-homelab` (`services.yggdrasil`), настроить подключение к
      публичным peers для достижимости адреса из интернета.
- [x] Стабильную идентичность ноды (private key в `yggdrasil.conf`) хранить в agenix: новый
      секрет `yggdrasil-keys` в `nodes/mytecor-homelab/secrets/`, подключить через
      `age.secrets` в `config.nix` и `publicKeys` в `secrets.nix`. Адрес ноды в `200::/7` должен
      оставаться стабильным между перезагрузками.
- [x] Продумать куда пишется `yggdrasil.conf`: генерировать из age-секрета или держать
      конфигурационный каталог в `/persist` — зафиксировать решение и обоснование.
- [ ] Добавить поддомены `myt.su`: DNS (внешний) AAAA-записи `*.myt.su` (или отдельные
      `service.myt.su`) указывают на yggdrasil-адрес ноды.
- [x] Расширить Caddy-ингress (`profiles/tcp-gateway` / маршруты сервисов), чтобы сервисы F4
      обслуживались по заголовку `Host` для `*.homelab.myt.su` параллельно LAN-контракту
      `*.local:80`. Решено: тот же порт 80 (HTTP); 443/TLS включается опционально через
      Cloudflare DNS-01 (`cloudflareToken`).
- [ ] Проверить доступ с клиента, находящегося в той же yggdrasil-сети (например с Mac), по
      каноническому имени `http://<service>.myt.su/`.
- [x] Обновить документацию: `nodes/mytecor-homelab/README.md`, `profiles/tcp-gateway/README.md`
      и при необходимости `ARCHITECTURE.md` (секция «Прикладной HTTP ingress»).

## Критерий готовности (Definition of Done)

- [ ] С внешней машины в yggdrasil-сети `curl --fail http://<service>.homelab.myt.su/` возвращает тот же
      ответ, что и `http://<service>.mytecor-homelab.local/` в LAN. **Блокировано:** AAAA-записи
      `*.homelab.myt.su` ещё не настроены во внешнем DNS (оператор настраивает вручную).
- [x] Yggdrasil-адрес и публичный ключ ноды стабильны после перезагрузки (private key приходит из
      agenix, а не генерируется заново).
- [x] Закрытый yggdrasil-ключ не попадает в Git: лежит только в виде `.age`-шифротекста, открытые
      значения и вывод их расшифрования не коммитятся.

## Затрагиваемые файлы / слои

- `nodes/mytecor-homelab/` (`config.nix`, `secrets/secrets.nix`, новые `secrets/yggdrasil-keys.age`)
- `profiles/tcp-gateway/` (Caddy-маршруты по `Host: *.myt.su`), при необходимости `profiles/networking`
- Внешний DNS `myt.su` (вне репозитория)
- Документация: `nodes/mytecor-homelab/README.md`, `profiles/tcp-gateway/README.md`, `ARCHITECTURE.md`

## Открытые вопросы

- Где хостится и как управляется DNS зоны `myt.su`? AAAA-записи `*.homelab.myt.su` на
  yggdrasil-адрес ноды (`200:e9f0:e122:7db:3bea:cf88:cdf3:fb91`) ведут на mesh-доступ
  (клиенты только из yggdrasil-сети). HTTPS опционален: с Cloudflare-токеном
  (`caddy-cloudflare-token.age`) mesh-сайты обслуживаются по TLS через DNS-01, порт 443
  открывается автоматически.
- Какие именно сервисы F4 выводятся наружу — решено: `acp`, `git-cache-proxy`, `radicle`,
  `status` идут на `*.homelab.myt.su`; `grafana` и `llm-gateway` в mesh НЕ выпускаются
  (`meshExclude` — нет публичной TLS/API-key защиты), остаются только на `*.local`.
  DNS-зона `homelab.myt.su` создаётся оператором вручную; репозиторий её не содержит.

## Принятые решения

- **yggdrasil.conf**: NixOS module генерирует конфиг на основе декларативных параметров
  (`Peers`, `IfName`, etc.) и подставляет `PrivateKeyPath`; systemd credentials
  (`LoadCredential`) подключают приватный ключ из agenix (`/run/agenix/yggdrasil-keys`).
