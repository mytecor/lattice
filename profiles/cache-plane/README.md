# Cache and artifact plane (F9).

Профиль включает сервисы cache-plane: Git cache proxy (f9-01), а в
дальнейшем — Verdaccio (f9-03) и Attic (f9-04). Long-lived accelerators
отделяются от ценных результатов: всё, что здесь живёт, —
disposable, удалимо и восстановимо из upstream/lockfiles.

Not a source of truth.

## Git cache proxy

По умолчанию upstream — публичный GitHub; конкретный origin задаёт нода.
Порт берётся из общего реестра `profiles/networking/ports.nix`.

### Repo-scoped authorization (f9-02)

Профиль по умолчанию оставляет `lattice.git-cache-proxy.allowRepos = []`
(serve-anything, до-f9-02 поведение), чтобы модульный контракт и VM-тесты могли
проверять обе моды. Продакшн-нода обязана задать точный allowlist; upstream
credential без непустого `allowRepos` отклоняется module assertion. См.
[патч пакета](../../packages/git-cache-proxy/README.md) и
[f9-02](../../docs/roadmap/f9-cache-artifact-plane/f9-02-git-repository-access.md).
