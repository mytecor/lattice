# f17-01. Каркас модуля `hotspot-switch` и конечный автомат

Скелет нового модуля `modules/hotspot-switch` с декларативными опциями и детерминированным конечным
автоматом режимов ноды (client/ap), без подъёма самой точки доступа.

Движет [F17 — динамический режим ноды: Ethernet → Wi-Fi точка доступа](./README.md).

## Контекст

Снятый модуль `wireless-hotspot` делал **одновременный** STA+AP и удалён как нестабильный на
RTL8822CE (`#channels <= 1`). F17 переключает режим **целиком**: провод есть → Wi-Fi только AP;
провода нет → Wi-Fi только STA. Эта задача создаёт каркас (опции + система состояний), на который
встанут детектор аплинка (f17-02) и подъём AP (f17-03).

## Что сделать

- [ ] Создать `modules/hotspot-switch/{options.nix,config.nix,default.nix}` по образцу снятого
      `wireless-hotspot`, но с моделью **переключения**, а не одновременности.
- [ ] Опции `lattice.hotspot-switch`:
      `enable`, `ethInterfaces` (listOf str), `wifiInterface`, `ap.{ssid,passwordFile,ip,routerIp,
      subnet,dhcpRange,dnsServers,channel,hwMode,macAddress,countryCode}`. Пароль — путь к agenix
      secret (в store не попадает).
- [ ] Конечный автомат с двумя состояниями `client`/`ap`, текущее состояние пишется в
      `/run/lattice-hotspot-switch/mode` идемпотентно.
- [ ] Программная защита `ap`-состояния: assertion, что STA-профиль не активен, когда поднят `ap0`
      (и наоборот), чтобы не воспроизвести нестабильность одновременного линка.
- [ ] Подключить модуль во `flake.nix` (input + `nixosModules`), добавить строку в
      `modules/README.md`.
- [ ] Изолированный тест на compile-time контракты (см. `tests/README.md`): опции валидируются,
      assertion работает.

## Критерий готовности (Definition of Done)

- [ ] `nix flake check --all-systems --no-build` проходит с подключённым модулем (enable=false по
      умолчанию) и без изменения существующих нод.
- [ ] Конечный автомат переключается между `client` и `ap` без подъёма реального AP (заглушка
      стартапа), состояние записывается в `/run`.

## Затрагиваемые файлы / слои

- `modules/hotspot-switch/{options,config,default}.nix` (новые)
- `flake.nix`, `modules/README.md`
- `tests/` (изолированный тест модуля)

## Открытые вопросы

_нет_
