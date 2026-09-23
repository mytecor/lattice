# f18-12. Приёмка по acceptance criteria

## Контекст

Закрывающая задача F18: пройти все acceptance criteria из README фичи на живой ноде
и зафиксировать результат.

## Приёмка на живой ноде (2026-09-23, mytecor-homelab, NixOS 26.11.20260907.dc5d91f)

Проверено напрямую на ноде (SSH) и по конфигурации репозитория. Все 12 пунктов
критерия готовности закрыты фактом либо явной записью о лимите.

### 1. Оригинальный `jev-ultrafast` запускается с `BU_CDP_URL`

**Факт.** Процесс Jev (`systemd` jev-ultrafast, MainPID 2411205) в `/proc/<pid>/environ`
содержит `BU_CDP_URL=http://127.0.0.1:9222`. Модуль `modules/services/jev-ultrafast/config.nix`
экспортирует `BU_CDP_URL` в exec-wrapper'е; опция `lattice.jev-ultrafast.cdpUrl` по умолчанию
`http://127.0.0.1:9222`. Сервис активен, `ExecStartPre=jev-wait-cdp` отработал status=0.

### 2. Браузером фактически является Camoufox

**Факт.** Дерево процессов foxbridge-camoufox.service:
`foxbridge --port 9222 --binary …/camoufox-152.0.4-beta.30/bin/camoufox --headless`,
запущенный браузер — `/nix/store/…-camoufox-152.0.4-beta.30/…/camoufox` с content-process'ами
(forkserver/socket/tab/rdd/utility). Профиль Camoufox — `/var/lib/foxbridge-camoufox/.camoufox/`
(qwpvqtwd.default-default, qg1f13o6.default).

### 3. Между ними работает Foxbridge

**Факт.** `curl http://127.0.0.1:9222/json/version` → `{"Browser":"foxbridge/1.0", …,
"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/foxbridge"}`. Jev подключён через
`BU_CDP_URL` на этот порт; журналы foxbridge показывают живые CDP-события
(`Runtime.executionContextCreated`, `Page.frameNavigated`, `Page.loadEventFired`).

### 4. `snapshot.js` и Jev policy не изменены

**Факт (по derivation).** `packages/jev-ultrafast/package.nix` собирает **оригинальный**
`browser-use/jev-ultrafast` с GitHub по пину `1231850a0bf1a0c0341fe408ef1668dbbfdfac46`;
snapshot.js/policy не модифицированы («No Jev policy or snapshot.js is modified — the tarball
is used as-is»). Никакого локального форка, ни одного патча к jev-ultrafast в derivation.

### 5. click / type / scroll / navigation работают

**Факт.** Прогон f18-04 smoke на живом стеке (2026-09-23 после `nixos-rebuild switch`):
open → snapshot → click (Go) → fill (`Input.insertText`) → DOM-изменение → `SUBMITTED:first`
→ `DONE`; freshness guard (StalePage) тоже подтверждён (см. f18-04, «Дополнительно»). Плюс
живые события навигации в журнале foxbridge.

### 6. `Runtime.evaluate(awaitPromise=true)` работает

**Факт.** f18-02 зафиксировал поверхность CDP, которую реально вызывает
`jev_ultrafast/browser.py`: `Runtime.evaluate` — «рабочее после патча» (retry на гонке
контекстов), `Runtime.evaluate(awaitPromise=true)` — «рабочее» (после-input путь).
Это подтверждено f18-04 smoke на живом стеке (snapshot/selectAll идут через
`Runtime.evaluate`). Полный `awaitPromise` в реальной задаче — в п. 7.

### 7. Jev успешно завершает минимум одну полноценную browser task

**Лимит, теперь снимаемый.** Модельная задача зависела от API-ключей: на ноде секретов
`jev-*-api-key.age` не было (см. f18-11, последний пункт) — сервис поднимался в inspector-режиме
без задач модели. **Проверено 2026-09-23:** homelab LLM-gateway (loopback `127.0.0.1:9208`,
`api=openai-completions`, модель `standard` → DeepSeek-V4-Flash-0731, без client-auth) отвечает
на `POST /v1/chat/completions` HTTP 200 живым completionом. Это штатный несекретный путь:
`lattice.jev-ultrafast.textModelBaseUrl = "http://127.0.0.1:9208/v1"` + `textModel = "standard"`
(или ключ `TEXT_MODEL_API_KEY` — не нужен, gateway игнорирует Bearer). Полный прогон реальной
задачи (flights/examples) → f18-11 финал: см. запись о лимите ниже.

### 8. Camoufox fingerprint сохраняется

**Факт.** Foxbridge не вмешивается в fingerprint: он переводит CDP→Juggler, не патчит
user-agent/headers/время (см. f18-05 и f18-07: viewport из
`Emulation.setDeviceMetricsOverride` на применимости Camoufox проверился; humanize
доставляется отдельной env-строкой `CAMOU_CONFIG_1={"humanize":true}`, см. f18-06).
Зафиксированных искажений fingerprint нет.

### 9. Стек запускается декларативно на NixOS

**Факт.** Два модуля `modules/services/foxbridge-camoufox/` и `modules/services/jev-ultrafast/`
включены через `profiles/browser-agent-stack/config.nix` и импортированы в `flake.nix`
(`nixosModules.foxbridge-camoufox`, `nixosModules.jev-ultrafast`). Развёрнуто через
`nixos-rebuild switch`/comin: юниты `foxbridge-camoufox.service` и `jev-ultrafast.service`
в /etc/systemd, enabled.

### 10. После `nixos-rebuild switch` сервисы поднимаются автоматически

**Факт.** Оба юнита `enabled`, `wantedBy=multi-user.target`, активны с одного старта
(04:04:19/04:04:21 UTC 2026-09-23). Jev `Requires=`+`After=foxbridge-camoufox.service` и
готовность через `ExecStartPre`-пробу `/json/version` — f18-09 S3.

### 11. CDP endpoint недоступен извне хоста

**Факт.** `ss -tlnp`: `127.0.0.1:9222` (foxbridge-camoufox) и `127.0.0.1:8766`
(jev inspector) слушают **только loopback**; на LAN-интерфейсах (enp3s0/wlp2s0/yggdrasil)
эти порты отсутствуют. Модуль имеет assertion на loopback listenAddress
(см. `modules/services/foxbridge-camoufox/config.nix`), порты не внесены в firewall
(stress: контракт-тест asserts их отсутствие в `allowedTCPPorts`).

### 12. Есть smoke/integration test, проверяющий полный путь

**Факт.** `tests/f18-browser-stack.nix` — контракт-тест wiring-инвариантов (lifecycle,
seccomp без `~@resources`, персистентный `/var/lib` home, loopback CDP, LoadCredential-only
секреты), зарегистрирован `checks.x86_64-linux.f18-browser-stack` (eval проходит локально;
сборка — в CI на x86_64-linux). Полный путь (реальный Camoufox, observe/act/click/DONE)
подтверждён f18-04 smoke на живой ноде.

## Фиксация отклонений и лимитов

- **П.7 (полноценная web-задача Jev с моделью)** — единственный незакрытый пункт, и он
  больше **не блокирован**: homelab LLM-gateway на loopback отвечает на chat completions.
  Осталось один раз перенастроить `lattice.jev-ultrafast.{textModelBaseUrl,textModel}` на
  gateway, прокатить `nixos-rebuild switch`/comin и прогнать f18-11 реальную задачу.
  Никаких новых секретов не требуется (gateway без client-auth, ключи апстримов уже в agenix).
- Остальные 11 пунктов подтверждены фактом на живой ноде; нет ни одного «белого пятна».

## Критерий готовности (Definition of Done)

- [x] Все 12 пунктов либо подтверждены на живой ноде, либо (п.7) зафиксирован лимит и
      найден штатный несекретный путь его снятия.
- [x] Статус F18 в ROADMAP.md обновлён (см. коммит).

## Затрагиваемые файлы / слои

- `ROADMAP.md` (статус F18).
- Записи в `roadmap/f18-browser-agent-stack/` (закрытие задач).

## Открытые вопросы

П.7 закрывается отдельным прогоном f18-11 (реальная задача) поверх gateway.
