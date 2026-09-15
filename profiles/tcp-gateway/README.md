# TCP gateway

Профиль включает Caddy и публикует активные HTTP-сервисы ноды. Канонический адрес каждого
доступного из LAN сервиса имеет вид:

```text
http://<service>.<node-name>.local/
```

Service-specific hostname публикуется через mDNS/Avahi. Все такие адреса используют HTTP-порт
`80`; единственный внешний listener принадлежит Caddy. Caddy маршрутизирует запрос по `Host` и
проксирует его на loopback backend либо обслуживает непосредственно. Порт backend не открывается
в firewall и не должен использоваться клиентами напрямую.

Публичные DNS/HTTPS-маршруты с явно заданным `networking.domain` являются отдельным ingress и не
заменяют обязательный LAN-адрес `service.node-name.local:80`.

Сейчас профиль автоматически добавляет reverse-proxy routes для `radicle-httpd`, HTTP control
plane `rns-server`, включённого LLM gateway, loopback-only Git cache proxy (F9) и Grafana
frontend (F12). Для LLM gateway создаётся Caddy-site
`http://llm-gateway.<node>.local/`; отдельный systemd service публикует этот hostname как mDNS
address alias через Avahi. Backend gateway продолжает слушать только loopback, его порт не
открывается в firewall. Прикладной node-status route, обслуживаемый самим Caddy, добавляет
[`profiles/app-services`](../app-services/README.md). Grafana (F12 observability) проксируется
с `http://grafana.<node>.local/` на loopback `127.0.0.1:9215` и публикует свой hostname в mDNS
тем же паттерном; порт 9215 в firewall не открывается.
