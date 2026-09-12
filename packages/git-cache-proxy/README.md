# git-cache-proxy (Lattice-patched)

Обёртка над pinned upstream
[`rolandjitsu/git-cache-proxy`](https://github.com/rolandjitsu/git-cache-proxy)
**v0.1.12** (rev `da96b57f84260c821fd6430a7845168245619ef3`), собираемая
`rustPlatform.buildRustPackage`. Runtime PATH получает закреплённый `git` через
makeWrapper (весь wire protocol делегируется системному `git`).

## Lattice-патч: repo-scoped authorization (f9-02)

Upstream 0.1.12 имеет только глобальный `serve-token` и никак не ограничивает,
*какие* репозитории запрос может читать: один upstream credential читает всё,
что он может достичь. Патч [`repo-allowlist.patch`](./repo-allowlist.patch)
добавляет повторяемый флаг `--allow-repo <path>`:

- Каждый обслуживающий путь — git `info/refs`, `git-upload-pack`, git-LFS
  `batch`, git-LFS object — отказывает репозиторию вне списка ответом **404**
  до какого-либо upstream fetch или чтения cache.
- 404 (а не 403) намеренно: запрещённый репозиторий неотличим от
  несуществующего, клиент не узнаёт, что mirror уже в cache.
- Семантика — точное совпадение пути, без префиксов/суффиксов/glob: entry
  `a/b.git` не авторизует `a/b-other.git`; пустой список = обслуживать всё
  (до-f9-02 поведение). Логика в `src/allowed.rs` и покрыта unit-тестами.

Обоснование и критерии готовности: [f9-02](../../docs/roadmap/f9-cache-artifact-plane/f9-02-git-repository-access.md),
применение через модуль — [modules/git-cache-proxy](../../modules/git-cache-proxy/README.md).

Патч применяется в сборке (`patches = [ ./repo-allowlist.patch ]`). При
обновлении версии заново портируйте/проверяйте патч (`git apply --check`).
