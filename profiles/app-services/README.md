# Прикладные сервисы

Профиль подключает первый прикладной payload узла и импортирует
[`tcp-gateway`](../tcp-gateway/README.md). Endpoint `lattice-node-status` обслуживается напрямую
директивой `respond` в Caddy: отдельный backend-процесс, внутренний порт и persistent state ему
не нужны.

Для ноды `<node>` с доменом `<domain>` endpoint имеет адрес:

```text
http://status.<node>.<domain>/
```

Если `networking.domain` не задан, используется домен `lattice`. Ответ содержит только публичное
имя ноды и имя сервиса:

```json
{"node":"mytecor-homelab","service":"lattice-node-status"}
```

Для проверки с машины без DNS-записи подставьте IP ноды через `curl --resolve`:

```sh
curl --fail --resolve status.mytecor-homelab.lattice:80:<NODE_IP> \
  http://status.mytecor-homelab.lattice/
```

В HTTP-клиенте, который позволяет вручную задавать заголовки (например, Yaak), запись в
`/etc/hosts` не нужна: отправьте `GET http://<NODE_IP>/` с заголовком
`Host: status.<node>.lattice`.

Профиль открывает только стандартные порты gateway; отдельного backend-порта нет.
