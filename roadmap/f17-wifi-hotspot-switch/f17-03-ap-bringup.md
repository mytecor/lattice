# f17-03. Подъём AP (hostapd/dnsmasq/NAT) и откат

Переключение радио в режим точки доступа: выключить STA, поднять виртуальный `ap0` + hostapd +
dnsmasq и NAT наружу через проводной аплинк; при потере провода — корректно вернуться в клиентский
режим.

Движет [F17 — динамический режим ноды: Ethernet → Wi-Fi точка доступа](./README.md).

## Контекст

В `ap`-состоянии Wi-Fi-радио свободно (STA выключен), поэтому можно фиксировать канал/полосу без
ограничения `#channels <= 1`, которое ломало одновременный режим (см. примечание в README фичи).
Переиспользуем проверенные приёмы снятого `wireless-hotspot`: уникальный локально-администрируемый
MAC для `ap0` (иначе RTL8822CE откажет в UP), hostapd в foreground под systemd, dnsmasq с
`bind-interfaces` на `ap0`, идемпотентный MASQUERADE.

## Что сделать

- [ ] Переход в `ap`: `nmcli device disconnect $wifi` → интерфейс unmanaged → `iw phy ... interface
      add ap0 type __ap` с уникальным MAC → `hostapd.conf` из age-секрета пароля (фиксированные
      `channel`/`hwMode`) → hostapd + dnsmasq + FORWARD → идемпотентный NAT/MASQUERADE через
      проводной аплинк.
- [ ] Откат: остановить hostapd/dnsmasq, удалить `ap0`, вернуть интерфейс в managed — NetworkManager
      сам переподключит Wi-Fi-профиль (autoconnect-priority из `lattice.wireless` уже ранжирован).
- [ ] Идемпотентность: повторный вход/выход — no-op; `RemainAfterExit` на one-shot юнитах.
- [ ] Rollback при сбое: если hostapd не поднялся за таймаут → вернуться в `client`, оставить Wi-Fi
      в managed (не оставлять ноду без сети).
- [ ] mDNS (avahi) на `ap0` и публикация `.local`-адресов сервисов ноды в hotspot-подсети —
      согласовать с `profiles/tcp-gateway`.
- [ ] Программная проверка отсутствия одновременности STA+AP (assertion из f17-01) в рантайме.

## Критерий готовности (Definition of Done)

- [ ] Клиент, подключённый к hotspot SSID, получает IP от dnsmasq, NAT до интернета через провод,
      нода отвечает по `.local` — при этом STA-линк выключен (одновременного Wi-Fi-клиента нет).
- [ ] После выдёргивания провода нода возвращается в Wi-Fi-клиентский режим автоматически, и
      отсутствие `ap0` подтверждено (`ip link`).
- [ ] Сбой подъёма hostapd не оставляет ноду без сети (rollback в `client`).

## Затрагиваемые файлы / слои

- `modules/hotspot-switch/config.nix`
- `modules/hotspot-switch/options.nix` (канал/полоса, ssid, пароль)
- `modules/wireless` (гарантия: STA-профиль не активен в `ap`-состоянии)

## Открытые вопросы

- Нужен ли отдельный `.local`-домен/подсеть hotspot, чтобы клиенты хотспота видели сервисы ноды
  (`*.local` через mDNS на `ap0`)?

  → Пока да: поднять avahi на `ap0` и публиковать те же алиасы, что и на проводе.
