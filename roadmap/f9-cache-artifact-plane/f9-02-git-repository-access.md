# Ограничить Git proxy по репозиториям

Фича: [F9 — cache и artifact plane](./README.md). Зависит от f9-01.

## Контекст

Proxy-side upstream credentials не должны превращать доступ к одному репозиторию в доступ ко всем
private Git objects. Workers не получают upstream GitHub credentials и не читают cache directory.

## Что сделать

- [x] Определить client identity и repo-scoped authorization policy.
      Механизм client identity (serve-token) в кандидате глобальный: он открывает всё, что может
      прочитать upstream credential. Репо-скоуп даётся отдельно — list точных путей репозиториев в
      Lattice-патче (`packages/git-cache-proxy/repo-allowlist.patch`): точное совпадение пути, без
      префиксов/суффиксов/glob; пустой список = serve-anything (до-f9-02).
- [x] Разделить proxy-side upstream credentials по минимально необходимым repositories.
      Узел homelab сейчас не держит upstream credential на прокси (публичный `mytecor/lattice`
      фетчится анонимно), поэтому разделять пока нечего; модульный assertion запрещает включать
      credential без непустого `allowRepos`, чтобы future per-repo credential не оказался в
      shared-reader режиме случайно.
- [x] Закрыть прямой filesystem и network доступ workers к bare mirrors и upstream credentials.
      Уже закрыто в f9-01 (system user, 0700 cache root, loopback-only, LoadCredential); сохранено.
- [x] Проверить allow/deny cases и отсутствие данных запрещённого repo в ответах/cache metadata.
      Rust unit-тесты (`src/allowed.rs` + `tests/http.rs`: denied → 404 до upstream, allowed →
      проходит в upstream, точность без suffix-расширения) и VM-тест
      `tests/git-cache-proxy.nix`: forbidden repo → 404, ничего не материализуется, mirror уже
      лежащий в cache того же denied repo → всё равно 404.

## Критерий готовности

- [x] Разрешённый client клонирует только явно выданные repositories.
- [x] Запрещённый repository и cache directory недоступны, даже если объект уже закеширован.

## Затрагиваемые файлы / слои

- `modules/git-cache-proxy/` — опция `allowRepos`, модульный assertion (credential ⇒ непустой
  allowlist), wrapper передаёт `--allow-repo` в argv.
- `packages/git-cache-proxy/` — Lattice-патч `repo-allowlist.patch` (f9-02) поверх pinned 0.1.12.
- `profiles/cache-plane/` — профиль по умолчанию оставляет `allowRepos = []` (serve-anything); нода
  задаёт список.
- `nodes/mytecor-homelab/` — `allowRepos = [ "mytecor/lattice" ]` (единственный origin-репозиторий).
- `KEY_MANAGEMENT.md` — правила для будущих per-repo upstream credentials.
- security checks — `tests/git-cache-proxy.nix`, `tests/git-cache-proxy-config.nix`.

## Открытые вопросы

Механизм client identity выбран (Lattice-патч, точный allowlist). Per-repo upstream credentials
для private repositories (агентский токен против нескольких репозиториев) — не реализуются, пока
на прокси нет ни одного upstream credential; когда понадобятся, список репо и credential должны
меняться одной фазой (см. KEY_MANAGEMENT.md).
