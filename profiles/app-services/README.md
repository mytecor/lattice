# Прикладные сервисы

Профиль подключает первый прикладной payload узла и импортирует
[`tcp-gateway`](../tcp-gateway/README.md). Endpoint `lattice-node-status` обслуживается напрямую
директивой `respond` в Caddy: отдельный backend-процесс, внутренний порт и persistent state ему
не нужны.

Для ноды `<node>` endpoint имеет отдельный mDNS-адрес:

```text
http://status.<node>.local/
```

Avahi публикует service-specific address alias, поэтому дополнительная DNS-запись, IP адрес и
ручной заголовок `Host` клиенту не нужны. Ответ содержит только публичное имя ноды и имя сервиса:

```json
{"node":"mytecor-homelab","service":"lattice-node-status"}
```

Проверка с машины в той же LAN:

```sh
curl --fail http://status.mytecor-homelab.local/
```

Профиль открывает только стандартные порты gateway; отдельного backend-порта нет.
