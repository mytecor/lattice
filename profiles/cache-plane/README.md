# Cache and artifact plane (F9).

Профиль включает сервисы cache-plane: Git cache proxy (f9-01), а в
дальнейшем — Verdaccio (f9-03) и Attic (f9-04). Long-lived accelerators
отделяются от ценных результатов: всё, что здесь живёт, —
disposable, удалимо и восстановимо из upstream/lockfiles.

Not a source of truth.

## Git cache proxy

По умолчанию upstream — публичный GitHub; конкретный origin задаёт нода.
Порт берётся из общего реестра `profiles/networking/ports.nix`.
