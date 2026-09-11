# wireless-hotspot

Модуль для **одновременной** (concurrent) работы Wi-Fi **STA + AP** на одном
физическом радио — сценарий, проверенный живым тестом на ноде
`mytecor-homelab` с адаптером Realtek RTL8822CE (драйвер `rtw88_8822ce`).

STA-интерфейс (у нас `wlp2s0`) управляется как обычно через NetworkManager —
модуль его **не трогает**. Модуль добавляет второй виртуальный интерфейс
`ap0` (HostAP) на **том же** радио и обслуживает hotspot: hostapd для
802.11-точки доступа, dnsmasq для DHCP, и NAT/MASQUERADE для интернета
клиентам.

## Зачем отдельный модуль

У `rtw88_8822ce` только один радио-канал:

```
valid interface combinations:
       * #{ managed } <= 1, #{ AP } <= 1,
         total <= 2, #channels <= 1
```

Это накладывает три обязательных ограничения, которые модуль учитывает:

1. **AP-интерфейс создаётся с уникальным MAC** (не MAC STA-интерфейса),
   иначе драйвер отказывается поднять его:
   `RTNETLINK answers: Name not unique on network` / `Could not set interface ap0 flags (UP)`.
2. **AP работает в простом 802.11n (HT) формате, даже на 5 GHz** — на
   `rtw88_8822ce` одновременный STA+AP стабилен только в HT-формате: VHT20
   (20 MHz) роняет data path, а VHT80 (80 MHz) валит сам hostapd. Поэтому
   модуль по умолчанию НЕ включает `ieee80211ac`; VHT — opt-in через опцию
   `lattice.hotspot.vht = true` (с `vht_oper_chwidth=0`, 20 MHz).
3. **Канал AP совпадает с каналом STA** — `#channels <= 1`, переключение радио
   оборвёт STA. Канал задаётся опцией `channel` и должен равняться каналу
   домашней сети.

Также NetworkManager пытается перехватить новый Wi-Fi интерфейс и перевести
его в managed — модуль помечает `ap0` как **unmanaged**
(`networking.networkmanager.unmanaged`), чтобы NM и hostapd не дрались за радио.

## Использование

```nix
{
  imports = [ self.nixosModules.hotspot ];

  age.secrets.hotspot-password = {
    file = ./secrets/hotspot-password.age;   # WPA2-PSK, 8-63 символа
    mode = "0400";
  };

  lattice.hotspot = {
    enable = true;
    ssid = "Mytecor Homelab";
    passwordFile = config.age.secrets.hotspot-password.path;
    # Канал/полоса по умолчанию определяются автоматически из канала STA —
    # hotspot встанет на ту же полосу и канал, что и живая STA-связь
    # (2.4 или 5 GHz). Явный override — см. таблицу опций ниже.
  };
}
```

По умолчанию канал и полоса AP **авто-определяются** из текущей STA-связи
(через `iw dev <staInterface> info`) при генерации конфига: hotspot встаёт на ту
же полосу и канал, что и STA. Это обязательно, т.к. `#channels <= 1` — AP не
может уйти на другой канал, чем STA (форсирование несовпадающего канала даёт
`Failed to set beacon parameters` / `AP-DISABLED`). Если нужно жёстко зафиксировать
(например 5 GHz / 44), задайте `channel` и `hwMode` явно.

Пароль хранится в age-секрете и материализуется в `/run/lattice-hotspot/
hostapd.conf` при загрузке — в Nix store он не попадает. `ssid` не секрет и
задаётся строкой.

## Опции

| Опция | Дефолт | Описание |
|---|---|---|
| `enable` | `false` | Включить hotspot |
| `interfaceName` | `ap0` | Имя виртуального AP-интерфейса |
| `phy` | `phy0` | Радио для `iw phy interface add … type __ap` |
| `ssid` | — | Имя сети |
| `passwordFile` | — | Файл (age-секрет) с WPA2-PSK паролем |
| `channel` | `null` (авто) | Канал AP; если задан — принудительно. По умолчанию берётся из канала STA |
| `hwMode` | `null` (авто) | `a` = 5 GHz, `g` = 2.4 GHz; если задан — принудительно, иначе из канала STA |
| `countryCode` | `US` | 802.11d country code |
| `macAddress` | `02:0a:44:00:00:01` | Уникальный MAC AP |
| `ip` | `10.44.0.1/24` | Адрес ноды на `ap0` |
| `routerIp` | `10.44.0.1` | Шлюз для DHCP-клиентов |
| `subnet` | `10.44.0.0/24` | Подсеть hotspot (для MASQUERADE-исключения) |
| `dhcpRange` | `10.44.0.10,10.44.0.100,255.255.255.0,12h` | Пул DNS/DHCP |
| `dnsServers` | `[ 1.1.1.1 8.8.8.8 ]` | Upstream DNS для клиентов |
| `staInterface` | `wlp2s0` | Имя STA-интерфейса (документация/проверки) |

## Что реализует модуль

- `systemd.services.lattice-hotspot` — создаёт `ap0` (`iw` + `ip`, уникальный
  MAC, назначение адреса), в `preStart` генерирует `hostapd.conf` из age-секрета
  пароля и текущего канала STA, затем запускает hostapd в foreground под systemd
  (`Restart=on-failure`). Конфиг перегенерируется на каждом старте/рестарте,
  поэтому канал AP всегда совпадает с текущим каналом STA.
- `services.dnsmasq` — DHCP + DNS на `ap0`.
- `networking.firewall.extraForwardRules` — FORWARD для клиентов hotspot.
- `systemd.services.lattice-hotspot-nat` — идемпотентный MASQUERADE в `-t nat`
  (NixOS firewall не имеет nat-хука).
- `boot.kernel.sysctl."net.ipv4.ip_forward" = "1"`.
- `networking.networkmanager.unmanaged` для `ap0`.

## Проверка после применения

```sh
iw dev                      # и wlp2s0 (managed), и ap0 (AP)
ip -br addr show ap0        # 10.44.0.1/24
systemctl status lattice-hotspot        # hostapd активен
journalctl -u lattice-hotspot           # нет Could not set channel / not unique
ss -ulnp | grep 5353        # avahi слушает ap0 (mDNS до узла по .local)
```

См. также [ARCHITECTURE.md](../ARCHITECTURE.md) и
[`modules/wireless`](../wireless/README.md) — модуль STA-подключения, который
этот модуль дополняет.
