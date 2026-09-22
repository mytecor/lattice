# f18-07. Черновики systemd-юнитов «вне NixOS»

Разделение на два независимых systemd-сервиса, связь — строго через `BU_CDP_URL`.
Эти unit-файлы — черновик для [f18-08](../f18-08-nixos-module.md): на них строится
деклративная модульная форма. Здесь они установлены руками на ноду, вне NixOS.

## Состав

| Файл | Роль |
| --- | --- |
| [`foxbridge-camoufox.service`](./foxbridge-camoufox.service) | Browser runtime: Foxbridge v6 → Camoufox (Juggler), CDP на `127.0.0.1:9222`. |
| [`jev-ultrafast.service`](./jev-ultrafast.service) | Upstream Jev (inspector на `127.0.0.1:8766`), включён через `BU_CDP_URL`. |

Зависимость Jev от рантайма — `Requires=` + `After=` + страховка по готовности
CDP в `ExecStartPre` (реальный `curl /json/version`, не тайминги).

## Проверено на ноде 2026-09-22

Оба сервиса поднялись и работали в systemd:

```sh
systemctl is-active foxbridge-camoufox.service jev-ultrafast.service   # active active
mkdir -p /run/systemd/system && cp systemd/*.service /run/systemd/system/   # юниты лежат здесь
systemctl daemon-reload
systemctl start foxbridge-camoufox.service jev-ultrafast.service
```

Порядок подъёма: Jev стартует только после готовности рантайма (`Requires=`/`After=` +
`ExecStartPre`, дождавшийся `curl /json/version`). CDP отвечает `foxbridge/1.0`,
инспектор Jev — `{"text_model":"deepseek-chat","status":"idle"}`.

## Установка на ноду (руками, вне NixOS)

```sh
# на ноде
# на ноде: /etc/systemd/system — это симлинка на /etc/static/systemd/system (NixOS-managed),
# во временные юниты кладём в /run/systemd/system/ (uniits загрузятся из /run)
scp systemd/foxbridge-camoufox.service systemd/jev-ultrafast.service root@mytecor-homelab.local:/run/systemd/system/
systemctl daemon-reload
systemctl enable --now foxbridge-camoufox.service
systemctl enable --now jev-ultrafast.service
```

Проверка (см. f18-07 DoD):

```sh
systemctl status foxbridge-camoufox.service --no-pager
systemctl status jev-ultrafast.service --no-pager
curl -s http://127.0.0.1:9222/json/version        # {"Browser":"foxbridge/1.0",...}
curl -s http://127.0.0.1:8766/api/state            # {"text_model":"deepseek-chat",...}
```

`jev-ultrafast.env` (когда понадобятся ключи — `TYPESAFE_API_KEY`,
`TEXT_MODEL_API_KEY`) кладётся на ноду **руками** как
`/etc/jev-ultrafast.env` (600, root) и не попадает в репозиторий и Nix store.

## CDP недоступен извне хоста

- Foxbridge **по построению** слушает только `127.0.0.1` (`pkg/cdp/server.go`
  hardcode `host: "127.0.0.1"`); внешняя публикация невозможна без патча.
- Дополнительная страховка: Jev-юнит подключён через тот же loopback, наружу
  CDP не пробрасывается ни одним юнитом.
- Проверка «с другой машины» в f18-07: попытка подключиться к узлу по его
  LAN/mesh-адресам (`enp3s0`, `wlp2s0`, `ygg0`) на порт 9222 должна падать.

## Изоляция (на будущее, для f18-08)

- Вариант жёстче — `PrivateNetwork=true` + `IPAddressAllow=lo` поверх loopback;
  для фонового детача не требуется, т.к. foxbridge и так слушает только 127.0.0.1.
- `KillMode=mixed` уже гарантирует, что при остановке умирает вся process group
  (Camoufox + content-процессы), zombie не остаётся.

## Отдельный runtime для daemon browser-harness

Jev рождает собственный daemon `browser-harness`. Чтобы он не пересекался
со случайно живущим вручную daemon'ом, юнит задаёт изолированный
`BH_RUNTIME_DIR=/root/.config/browser-harness/f18rt` (см. f18-04 smoke, где этот
приём уже проверен). Daemon резолвит `BU_CDP_URL → /json/version →
webSocketDebuggerUrl` и работает поверх общего Foxbridge.
