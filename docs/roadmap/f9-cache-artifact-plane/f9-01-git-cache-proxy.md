# Развернуть Git cache proxy

Фича: [F9 — cache и artifact plane](./README.md). Зависит от
[f4-01](../f4-payload/f4-01-radicle-seed-comin.md) и F8.

## Контекст

Кандидат `rolandjitsu/git-cache-proxy` должен ускорять повторные clone/fetch через локальные bare
mirrors. Cache хранится на локальной POSIX FS и не является source of truth.

## Что сделать

- [x] Проверить кандидата на нужных Git transport/auth flows и закрепить версию.
  Выбран `rolandjitsu/git-cache-proxy` **v0.1.12** (rev `da96b57f84260c821fd6430a7845168245619ef3`).
  Это Rust-прокси (не Go): read-only, lazy, pull-only, делегирует wire protocol системному `git`
  (v2/v0, shallow, partial), LFS кешируется content-addressed, есть Prometheus `/healthz|readyz|metrics`.
  Подтверждены: cold/warm clone (cache hit тот же commit graph), delta fetch, удаление mirror ->
  обычный refetch, read-only push -> 403, строгий песочник systemd. Package в
  [`packages/git-cache-proxy/`](../../../packages/git-cache-proxy/package.nix) собирается
  (`rustPlatform.buildRustPackage`, git в runtime PATH через makeWrapper).
- [x] Добавить NixOS service с отдельным пользователем и cache directory под `/var/cache`.
  Модуль [`modules/git-cache-proxy/`](../../../modules/git-cache-proxy/README.md): отдельный
  system user/group, tmpfiles создаёт `/var/cache/git-cache-proxy` 0700, upstream-credential и
  serve-token подаются через `LoadCredential` в env (`GITCACHEPROXY_*`), не argv.
- [x] Направить тестовый clone/fetch через proxy и подтвердить cache hit.
  См. [VM-тест](../../../tests/git-cache-proxy.nix): локальный `git http-backend` как origin,
  cold/warm clone, delta fetch (`fetchTtlSeconds = 0`), cache-loss refetch, read-only 403.
- [x] Ограничить сеть и filesystem права сервиса его назначением.
  Loopback-only bind (`127.0.0.1:<port>`), публикация наружу только Caddy-ингресс
  `tcp-gateway` (mDNS alias), `NoNewPrivileges`, `ProtectSystem=strict` с `ReadWritePaths`
  только на cache root, `CapabilityBoundingSet=""`, ограниченные syscalls/address families.

## Критерий готовности

- [x] Повторный clone/fetch использует локальный bare mirror и даёт тот же commit graph.
- [x] Удаление mirror приводит к обычному refetch, а не к потере source.

## Затрагиваемые файлы / слои

- [`modules/git-cache-proxy/`](../../../modules/git-cache-proxy/)
- [`profiles/cache-plane/`](../../../profiles/cache-plane/)
- [`nodes/mytecor-homelab/`](../../../nodes/mytecor-homelab/)

## Открытые вопросы

Окончательный выбор proxy подтверждён на публичном upstream (GitHub) и на локальном smart-HTTP
origin (VM-тест). Private repository auth flow и repo-scoped authorization — отдельная задача
[f9-02](f9-02-git-repository-access.md): у кандидата нет per-repo authorization (один serve-token
открывает всё, что читает upstream-credential), поэтому upstream-credential на ноде пока не
включён (homelab-инвариант в `tests/default.nix`).
