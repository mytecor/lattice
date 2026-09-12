# Git cache proxy

Модуль предоставляет `lattice.git-cache-proxy.enable` — systemd-сервис
[`rolandjitsu/git-cache-proxy`](https://github.com/rolandjitsu/git-cache-proxy),
read-only lazy caching proxy для Git-репозиториев.

Прокси сидит между клиентами и origin git-сервером: обслуживает clone/fetch из
локального bare mirror, а на каждый запрос дотягивает только **дельту** от
upstream. Read-only и pull-only — push отклоняется, а на origin ничего не
записывается и не реплицируется. Git wire protocol делегируется системному
`git` (v2/v0, shallow, partial/filtered clone), LFS-объекты кешируются
content-addressed. Метрики Prometheus: `/healthz`, `/readyz`, `/metrics`.

## Опции

- `lattice.git-cache-proxy.enable` — включить сервис.
- `lattice.git-cache-proxy.package` — пакет (по умолчанию
  `pkgs.lattice.git-cache-proxy`, pinned `0.1.12`).
- `lattice.git-cache-proxy.user` / `group` — system user/group
  (по умолчанию `git-cache-proxy`).
- `lattice.git-cache-proxy.host` / `port` — адрес привязки (по умолчанию
  loopback); публикацию наружу делает Caddy-ингресс `tcp-gateway`.
- `lattice.git-cache-proxy.upstream` — origin git base URL (например
  `https://github.com`), к нему добавляется путь репозитория.
- `lattice.git-cache-proxy.upstreamAuthHeaderFile` — runtime path к секрету с
  полным HTTP-заголовком для upstream (например `Authorization: Basic …`),
  монтируется через systemd `LoadCredential`, в argv не попадает.
- `lattice.git-cache-proxy.serveTokenFile` — runtime path к bearer-токену для
  клиентов (опционально); `null` = анонимно в пределах закрытой сети.
- `lattice.git-cache-proxy.allowRepos` — repo-scoped authorization (f9-02):
  список точных путей репозиториев, которые прокси может обслуживать
  (например `[ "mytecor/lattice" ]` при `upstream = "https://github.com"`).
  Пустое значение = обслуживать что угодно (до-f9-02 поведение). Запрос к
  любому репозиторию вне списка отклоняется 404 до какого-либо upstream fetch
  или чтения cache, даже если mirror уже материализован.
- `lattice.git-cache-proxy.cacheRoot` — каталог bare mirrors (по умолчанию
  `/var/cache/git-cache-proxy`, disposable).
- `lattice.git-cache-proxy.runtimeDirectory`, `fetchTtlSeconds`, `cacheMaxMb`,
  `maxConcurrentRequests`, `maxDecodedBodyMb` — см. README кандидата.

## Границы безопасности

Прокси — разделяемый читатель с одним upstream-credential. **Reachability —
это граница доверия**: у кандидата нет per-repo authorization, один
`serve-token` открывает всё, что может прочитать upstream-credential. Поэтому:

1. Сервис слушает только **loopback** (`127.0.0.1:<port>`), наружу публикуется
   ингрессом `tcp-gateway` (Caddy) через mDNS alias.
2. Upstream-credential и serve-token живут вне Nix store (agenix `.age`), в
   сервис попадают через `LoadCredential` и экспортируются в **env**, не в
   argv (прокси читает флаги из `GITCACHEPROXY_*`).
3. Строгий песочник systemd: `NoNewPrivileges`, `ProtectSystem=strict`
   (`ReadWritePaths` — только cache root), `MemoryDenyWriteExecute`,
   ограниченные system calls и address families.
4. Workers не получают upstream-credentials и не читают cache directory —
   команда выполняется от отдельного system user `git-cache-proxy`.

Repo-scoped authorization (f9-02) вынесен отдельной задачей — у кандидата его
нет, и он не может быть добавлен только конфигурацией. Lattice патчит pinned
`0.1.12` (`packages/git-cache-proxy/repo-allowlist.patch`), добавляя флаг
`--allow-repo` (repeatable). Модуль передаёт `allowRepos` как repeatable argv
(это публичные пути, не секреты); upstream credential по-прежнему идёт только
через `LoadCredential`/env. Модульный assertion запрещает upstream credential
без непустого `allowRepos`.

## Кеш — не source of truth

Bare mirrors — disposable: удаление cache вызывает обычный refetch, а не потерю
source. См. раздел «Доказать disposable-семантику caches» (f9-06).
