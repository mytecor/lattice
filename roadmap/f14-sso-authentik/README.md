# F14. SSO через Authentik (центральная идентификация пользователей)

Центральный single sign-on для пользовательских web-сервисов ноды. Authentik разворачивается
нативно через NixOS (собственный модуль поверх `pkgs.authentik`), стоИт за существующим Caddy —
единственным внешним ingress — и становится единым источником истины для «кто может открыть
интерфейс сервиса»: сервисы без собственного SSO заворачиваются в Caddy ForwardAuth, сервисы со
встроенным OIDC/OAuth (Grafana) подключаются к нему нативно. Machine-to-machine API на
browser-based flow Authentik **не завязывается**; при этом web-UI тех API-сервисов, у которых UI и
API разделены, может защищаться отдельно, не трогая API endpoints.

Соответствует [вехе 14](../../ROADMAP.md#f14-sso-через-authentik-центральная-идентификация-пользователей).

Задачи: [f14-01](./f14-01-deploy-authentik-sso.md) — развёртывание Authentik нативно через NixOS и
встраивание его как центрального SSO: собственный модуль поверх `pkgs.authentik`, секреты через
agenix, Caddy-ингресс, Caddy ForwardAuth для сервисов без собственного SSO, нативный OIDC для
Grafana, защита только web-UI API-сервисов и разделение «люди vs machine-to-machine». Дальнейшие
доработки (user lifecycle, multi-node) — отдельные follow-up.

**Критерий готовности:** один вход в Authentik через
`https://auth.<meshDomain>/` (и `http://auth.<node>.local/` на LAN) открывает защищённый веб-UI
всех подключённых сервисов; сервис без собственного SSO защищён Caddy ForwardAuth; Grafana
входит через OIDC provider Authentik; API machine-to-machine вызовы проходят независимо от
браузерной сессии; у API-сервиса с раздельными UI/API защищён только UI. Секреты (secret_key,
bootstrap token, password) — через agenix, ни один в Nix store не попадает.

**Осознанно откладываем (до F…):** полноценный user lifecycle / self-service recovery
(пока аккаунты заводит оператор), multi-node отказоустойчивый Authentik, proxy/outpost-деплой на
отдельных нодах, скоуп SSO на не-browser клиенты (API-ключи/TOTP для M2M — только там, где сервис
отдаёт их сам).

## Граница: что закрывает SSO, а что нет

Задача не трогает machine-to-machine путь: LLM gateway API-ключи, Radicle-подписи, rnsh/rns
идентичность узла и Yggdrasil-идентичность остаются как есть. Authentik — это про **людей**,
открывающих веб-интерфейс, не про сервисные credentials. Это правило уже закреплено в
[ARCHITECTURE.md](../../ARCHITECTURE.md#идентичность-узла) и [ARCHITECTURE.md](../../ARCHITECTURE.md#секреты):
идентичность узла не смешивается с пользовательской аутентификацией.
