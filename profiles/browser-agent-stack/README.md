# Browser agent stack (F18)

Профиль включает F18-стек декларативно на ноде:

- `lattice.foxbridge-camoufox.enable` — browser runtime (Foxbridge CDP-прокси +
  Camoufox) на loopback-порту `latticePorts.foxbridge-cdp` (9222),
  headed-браузер в приватном Xvfb + `humanize`. Программный Mesa обеспечивает
  WebGL без физического GPU; FFmpeg из [пакета Camoufox](./../../packages/camoufox/README.md)
  обеспечивает декодирование видео.
- `lattice.jev-ultrafast.enable` — агент Jev поверх `BU_CDP_URL`
  `http://127.0.0.1:9222`, loopback-инспектор на `latticePorts.jev-inspector`
  (8766).

Backend-порты обоих сервисов слушают только loopback. Для отладки CDP доступен
через защищённый [browser-сайт](./../moli/README.md) и маршрут `/foxbridge/`.
Xvfb не принимает TCP-соединения. Для возврата к нативному headless задайте
`camoufox.headless = true` и `camoufox.virtualDisplay = false` в настройках модуля.
Секреты Jev подключаются на узле через agenix (`typesafeApiKeyFile` /
`textModelApiKeyFile`); без них сервисы поднимаются в inspector-режиме.
