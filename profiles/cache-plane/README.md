# Cache and artifact plane (F9).

Профиль включает сервисы cache-plane: Git cache proxy (f9-01) и Verdaccio
npm/pnpm/yarn caching proxy (f9-03).
Long-lived accelerators
отделяются от ценных результатов: всё, что здесь живёт, —
disposable, удалимо и восстановимо из upstream/lockfiles.

Not a source of truth.

## Verdaccio npm caching proxy

По умолчанию cache-only: только loopback, anonymous read внутри закрытой
LAN, publish выключен. Пакеты кешируются в `/var/cache/verdaccio`;
клиенты ноды (npm/pnpm/yarn) направляются на `127.0.0.1:9212` из tool
profile (см. `profiles/pi`). Порт берётся из общего реестра
`profiles/networking/ports.nix`.

## Git cache proxy

По умолчанию upstream — публичный GitHub; конкретный origin задаёт нода.
Порт берётся из общего реестра `profiles/networking/ports.nix`.

### Repo-scoped authorization (f9-02)

Профиль по умолчанию оставляет `lattice.git-cache-proxy.allowRepos = []`
(serve-anything, до-f9-02 поведение), чтобы модульный контракт и VM-тесты могли
проверять обе моды. Продакшн-нода обязана задать точный allowlist; upstream
credential без непустого `allowRepos` отклоняется module assertion. См.
[патч пакета](../../packages/git-cache-proxy/README.md) и
[f9-02](../../roadmap/f9-cache-artifact-plane/f9-02-git-repository-access.md).
