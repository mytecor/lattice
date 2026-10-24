# Browser agent stack (F18)

Профиль включает F18-стек декларативно на ноде:

- `lattice.foxbridge-camoufox.enable` — browser runtime (Foxbridge CDP-прокси +
  Camoufox) на loopback-порту `latticePorts.foxbridge-cdp` (9222),
  headless-браузер с Headless EGL/GPU (`/dev/dri/renderD128`) + `humanize`.
  Аппаратный Mesa/iHD обеспечивает WebGL и VA-API декодирование видео.
- `lattice.jev-ultrafast.enable` — агент Jev поверх `BU_CDP_URL`
  `http://127.0.0.1:9222`, loopback-инспектор на `latticePorts.jev-inspector`
  (8766).

Backend-порты обоих сервисов слушают только loopback. Для отладки CDP доступен
через защищённый [browser-сайт](./../moli/README.md) и маршрут `/foxbridge/`.
Секреты Jev подключаются на узле через agenix (`typesafeApiKeyFile` /
`textModelApiKeyFile`); без них сервисы поднимаются в inspector-режиме.
