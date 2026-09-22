# Browser agent stack (F18)

Профиль включает F18-стек декларативно на ноде:

- `lattice.foxbridge-camoufox.enable` — browser runtime (Foxbridge CDP-прокси +
  Camoufox) на loopback-порту `latticePorts.foxbridge-cdp` (9222),
  headless + `humanize`.
- `lattice.jev-ultrafast.enable` — агент Jev поверх `BU_CDP_URL`
  `http://127.0.0.1:9222`, loopback-инспектор на `latticePorts.jev-inspector`
  (8766).

Оба сервиса слушают только loopback; CDP/инспектор наружу не публикуются.
Секреты Jev подключаются на узле через agenix (`typesafeApiKeyFile` /
`textModelApiKeyFile`); без них сервисы поднимаются в inspector-режиме.
