# f18-07. Разделить browser runtime и Jev на два systemd сервиса

## Контекст

В homelab Camoufox не должен жить внутри процесса Jev. Перед NixOS-модулем оформляем
минимальные systemd-юниты: отдельный long-running browser runtime и отдельный сервис Jev.
Связь — строго через `BU_CDP_URL`.

## Что сделать

- [ ] Сервис `foxbridge-camoufox.service`: сам Foxbridge, который поднимает Camoufox
      (Juggler backend) и слушает локальный CDP endpoint.
- [ ] Сервис `jev-ultrafast.service`: Jev, подключённый через
      `BU_CDP_URL=http://127.0.0.1:<foxbridge-port>`.
- [ ] Зависимость Jev от Foxbridge: `After=foxbridge-camoufox.service`,
      `Requires=foxbridge-camoufox.service`.
- [ ] Foxbridge слушает только localhost либо приватный network namespace; CDP endpoint
      наружу homelab не публикуется.
- [ ] Сервисы поднимаются через systemd на целевой машине (вне NixOS — руками).

## Критерий готовности (Definition of Done)

- [ ] Два независимых systemd-сервиса, Jev стартует только после готовности
      `foxbridge-camoufox`.
- [ ] CDP недоступен извне хоста (проверить с другой машины/интерфейса).

## Затрагиваемые файлы / слои

- Пока вне NixOS: systemd unit-файлы в репозитории фичи (черновик для f18-08).
- `profiles/`/`nodes/` не трогаем до f18-08.

## Открытые вопросы

_нет_
