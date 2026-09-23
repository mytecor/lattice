# f18-09. Lifecycle и восстановление

## Контекст

Foxbridge/Camoufox — отдельный long-running runtime. Нужно убедиться, что его падение
не роняет весь стек и что состояние корректно восстанавливается. На первом этапе —
**один** Camoufox instance, без pool и autoscaling.

## Модель наблюдения (установлено по исходникам Foxbridge)

Foxbridge — single-shot супервизор ровно одного Camoufox-процесса (`pkg/firefox/process.go`):

- `main.go` запускает Firefox через Juggler-pipe, затем ждёт в `select { <-sig, <-done }`,
  где `done` закрывается `proc.Wait()` при выходе браузера.
- Значит, **падение Camoufox → `proc.Wait()` разблокируется → main возвращается →
  Foxbridge завершается сам** (defer `proc.Stop()`).
- Восстановление после любой смерти браузера — забота systemd: юнит
  `foxbridge-camoufox.service` имеет `Restart=on-failure`, `RestartSec=3`,
  `KillMode=mixed`, и при падении Foxbridge (вместе с убитым браузером) перезапускает
  **всю пару**. In-process supervision / auto-relaunch в Foxbridge нет — это не потеря,
  а явная граница: поднимать новый экземпляр поручается init-системе, а не Go-мосту.
- Связка с агентом: `jev-ultrafast.service` имеет `Requires=`+`After=foxbridge-camoufox.service`
  и `ExecStartPre`, который опрашивает живой CDP `/json/version` (до 30 попыток × 2 c).
  Поэтому остановка/рестарт runtime корректно тянет за собой чистый рестарт Jev
  (без зависшего harness, без гонок на мёртвый WS).

## Что сделать (проверено на живой ноде 2026-09-23)

- [x] Проверить восстановление после падения Camoufox: `kill -9` на `camoufox --no-remote`
      → Foxbridge ловит выход браузера и самостоятельно завершается → `systemd Restart=on-failure`
      перезапускает пару → CDP `/json/version` снова отвечает, `foxbridge-camoufox` **active**,
      NRestarts=0 после восстановления, Jev перезапускается и встаёт. (S1)
- [x] Проверить восстановление после падения Foxbridge: `kill -9` на `foxbridge --port 9222`
      → systemd перезапускает пару, CDP поднимается за секунды, оба юнита active. (S2)
- [x] Проверить повторное подключение `browser-harness` после перезапуска рантайма:
      повторное подключение выполняется на уровне systemd, а не in-process. Т.к. Jev
      `Requires=`+`After=` runtime и стартует через `ExecStartPre`-пробу CDP, любой рестарт
      Foxbridge корректно останавливает и перезапускает Jev целиком (журнал:
      `Stopping jev-ultrafast` → `Starting` → `Jev Ultrafast: http://127.0.0.1:8766`), без
      зависших WS и без ручного вмешательства. Это и есть модель «повторного подключения»:
      не переподключение в процессе, а детерминированный чистый перезапуск. (S3)
- [x] Проверить отсутствие zombie browser processes: после `kill -9` Camoufox и Foxbridge
      zombie count = 0 на протяжении всех сценариев; `KillMode=mixed` собирает
      content-процессы. Осиротевших от прежних смертей браузера нет. (S4)
- [x] Проверить корректное закрытие target после `Agent.close()`: `Browser.close()` в Jev
      вызывает `Target.closeTarget(targetId)`. На живом CDP: until 2 page-target, после
      `Browser("about:blank")` — 3, после `close()` — снова 2 (созданный target закрыт). (S5)
- [x] Проверить, что падение Jev не завершает browser runtime некорректно: `kill -9` на
      `jev-wrapped` → runtime (`foxbridge-camoufox` + Camoufox + content-процессы) **остаётся
      живым и CDP отвечает**; Jev перезапускается systemd'ом и переиспользует тот же runtime. (S6)

## Критерий готовности (Definition of Done)

- [x] Все сценарии воспроизведены и восстановимы на живой ноде: S1..S6 проходят,
      стек после каждой смерти возвращается к `active + active` + CDP UP.
- [x] Нет zombie процессов (S4: 0 zombies во всех сценариях); `Agent.close()` закрывает target (S5).

## Вывод / зафиксированные наблюдения

Реальной проблемой lifecycle на этом пути оказался **нелицевой crash-loop при
не-готовности браузера (f18-08)**: seccomp-фильтр и tmpfs-HOME убивали Camoufox на старте,
Foxbridge ловил `client closed` в `Browser.enable` и падал, systemd рестартил с `RestartSec=3`
— restart counter уходил в десятки тысяч. Это было ПОЛНОСТЬЮ устранено в f18-08 серией
фиксов (207b0d8 → f39a707 → 06c6d4e): HOME на персистентный `/var/lib/foxbridge-camoufox`,
`--profile` убран, seccomp без `~@resources`. После применения `main` на ноде (comin,
генерация 03:48 UTC) стек стабилен (NRestarts=0 на момент проверки, CDP и Jev отвечают).

Наблюдение для записи: **boot-time health** юнита — это `ExecStartPre`-проба CDP у Jev;
у самого `foxbridge-camoufox` `Restart=on-failure` + `RestartSec=3` решают только
пост-стартовые падения. Если браузер систематически не поднимается (как в crash-loop),
systemd сам упирается в `StartLimitBurst` и останавливается — это намеренно, чтобы не
жрать CPU в бесконечном цикле; диагноз остаётся в журнале + coredump. Для f18-11
предлагается добавить контракт-тест на CDP-readiness (ExecStartPre-проба) как smoke.

## Затрагиваемые файлы / слои

- `modules/services/foxbridge-camoufox/config.nix`, `modules/services/jev-ultrafast/config.nix`
  (restart-политики и ExecStartPre уже реализованы и проверены).
- Правки Foxbridge не потребовались: архитектура single-shot + systemd-restart оказалась
  достаточной и предсказуемой.

## Открытые вопросы

- Ожидаемое поведение «после падения Camoufox» (поднимать ли новый instance сразу или ждать
  обращения Jev): принято — **сразу**, через systemd-restart всей пары, потому что Jev
  `Requires`-связан с runtime и всё равно стартует после него; «ленивый» restart дал бы
  только сложнее диагностику. In-process relaunch остаётся возможным right-sizing, если
  понадобится редюс downtime, но не требуется для текущих задач.
