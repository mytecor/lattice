# `lattice.hotspot-switch`

Модуль динамического переключения Wi-Fi-радио ноды между клиентским режимом (STA) и точкой доступа (AP) в зависимости от наличия проводного Ethernet-аплинка в интернет ([F17](../../roadmap/f17-wifi-hotspot-switch/README.md)).

## Зачем

Многодомная нода (проводной Ethernet `enp3s0` + Wi-Fi `wlp2s0`) при наличии кабеля получает прямой интернет по проводу. При этом её Wi-Fi-радио простаивает в режиме клиента домашней сети.

На используемом чипе (Realtek RTL8822CE, `#channels <= 1`) одновременный режим STA+AP нестабилен и приводит к деградации линка и сбоям драйвера. Поэтому модуль реализует **полное переключение режима**:
- **Проводной аплинк есть с default-route:** Wi-Fi-клиент (STA) отключается, радио переводится в режим точки доступа (AP `ap0`) с DHCP (`dnsmasq`), NAT/MASQUERADE наружу через провод и mDNS.
- **Провод отключён или потерял default-route:** точка доступа останавливается, интерфейс `ap0` удаляется, Wi-Fi возвращается под управление NetworkManager и автоматически подключается к известным клиентским сетям.
- В любой момент времени активно **ровно одно состояние** (STA **или** AP), исключая конфликт каналов.

## Архитектура

```text
[ Физический кабель ] -> systemd.path (/sys/class/net/enp3s0/carrier) ─┐
                                                                       ├─> lattice-hotspot-switch eval
[ NetworkManager ]    -> dispatcher.d (up, down, dhcp4-change)         ─┘             │
                                                                                      ▼
                                                                        [ Проверка default-route ]
                                                                        [ + debounce анти-флаппинг ]
                                                                                      │
                                                    ┌─────────────────────────────────┴────────────────────────────────┐
                                                    ▼                                                                  ▼
                                           [ Есть Ethernet uplink ]                                          [ Нет Ethernet uplink ]
                                                    │                                                                  │
                                                    ▼                                                                  ▼
                                   systemctl start lattice-hotspot-ap                                 systemctl stop lattice-hotspot-ap
                                                    │                                                                  │
                                   ┌────────────────┴────────────────┐                                ┌────────────────┴────────────────┐
                                   │ 1. nmcli disconnect STA         │                                │ 1. stop dnsmasq                 │
                                   │ 2. nmcli managed no STA         │                                │ 2. remove NAT MASQUERADE        │
                                   │ 3. assert STA disconnected      │                                │ 3. delete ap0                   │
                                   │ 4. create ap0 (unique MAC)      │                                │ 4. nmcli managed yes STA        │
                                   │ 5. start hostapd (channel 36)   │                                │ 5. restart *-mdns               │
                                   │ 6. start dnsmasq (10.44.0.0/24) │                                │ 6. mode = client                │
                                   │ 7. NAT MASQUERADE & forward     │                                └─────────────────────────────────┘
                                   │ 8. restart *-mdns               │
                                   │ 9. mode = ap                    │
                                   └─────────────────────────────────┘
```

### 1. Детектор аплинка и анти-флаппинг
- Отслеживание изменений через `systemd.path` на `/sys/class/net/<eth>/carrier` и NetworkManager Dispatcher скрипт на событиях `up`, `down`, `dhcp4-change`, `connectivity-change`.
- Оценка маршрутизации: `ip -4 route get 1.1.1.1` проверяет, что исходящий интерфейс принадлежит списку `ethInterfaces` и имеет carrier `1`.
- Защита от дребезга (debounce): при переходе из `client` в `ap` скрипт ожидает `DEBOUNCE_SEC` (по умолчанию 2 секунды) и повторно валидирует маршрут перед фиксацией состояния.

### 2. Защита от одновременности (Invariants)
- Перед созданием `ap0` клиентский интерфейс отключается в NetworkManager (`nmcli device disconnect`) и переводится в `managed no`.
- Скрипт проверяет через `iw dev <sta> link`, что соединение отсутствует. Если интерфейс не разорвал связь, он принудительно переводится в `down`.
- Виртуальный интерфейс `ap0` помечен как `networking.networkmanager.unmanaged = [ "ap0" ]`, чтобы NetworkManager не пытался им управлять.

### 3. Автоматический откат (Fail-safe Rollback)
- Если `hostapd` не смог запуститься или аварийно упал, systemd вызывает `ExecStopPost`, который удаляет `ap0`, возвращает клиентский Wi-Fi в `managed yes` и фиксирует состояние `client`.
- Нода никогда не остаётся без сетевого доступа при сбое радиомодуля точки доступа.

## Опции

| Опция | Тип | По умолчанию | Описание |
|---|---|---|---|
| `lattice.hotspot-switch.enable` | `bool` | `false` | Включение механизма динамического переключения хотспота. |
| `lattice.hotspot-switch.ethInterfaces` | `nullOr (listOf str)` | `null` (auto) | Список Ethernet-интерфейсов для отслеживания. `null` — автоматическое определение всех физических Ethernet-портов ноды. |
| `lattice.hotspot-switch.wifiInterface` | `nullOr str` | `null` (auto) | Имя интерфейса Wi-Fi-клиента (STA). `null` — автоматическое определение Wi-Fi интерфейса ноды. |
| `lattice.hotspot-switch.phy` | `nullOr str` | `null` (auto) | Имя физического радиоустройства. `null` — автоматическое определение из Wi-Fi интерфейса (например `phy0`). |
| `lattice.hotspot-switch.ap.ssid` | `str` | _(обязательно)_ | SSID раздаваемой Wi-Fi-сети. |
| `lattice.hotspot-switch.ap.passwordFile` | `path` | _(обязательно)_ | Путь к файлу с WPA2-паролем (agenix секрет). |
| `lattice.hotspot-switch.ap.channel` | `int` | `36` | RF-канал точки доступа (5 GHz). |
| `lattice.hotspot-switch.ap.channelWidth` | `enum [ 20 80 ]` | `20` | Ширина канала; 80 требует VHT и поддерживаемый primary channel. |
| `lattice.hotspot-switch.ap.hwMode` | `enum [ "a" "g" ]` | `"a"` | Режим радио: `a` (5 GHz) или `g` (2.4 GHz). |
| `lattice.hotspot-switch.ap.countryCode` | `str` | `"US"` | Код страны IEEE 802.11d. |
| `lattice.hotspot-switch.ap.macAddress` | `str` | `"02:0a:44:00:00:01"` | Локально-администрируемый MAC-адрес интерфейса `ap0`. |
| `lattice.hotspot-switch.ap.ip` | `str` | `"10.44.0.1/24"` | IP-адрес ноды на интерфейсе `ap0`. |
| `lattice.hotspot-switch.ap.subnet` | `str` | `"10.44.0.0/24"` | Подсеть хотспота. |
| `lattice.hotspot-switch.ap.dhcpRange` | `str` | `"10.44.0.10,10.44.0.100,255.255.255.0,12h"` | Пул адресов DHCP для клиентов. |
| `lattice.hotspot-switch.ap.dnsServers` | `listOf str` | `[ "1.1.1.1" "8.8.8.8" ]` | Upstream DNS-серверы для клиентов хотспота. |
| `lattice.hotspot-switch.ap.vht` | `bool` | `false` | Включить 802.11ac; вместе с `channelWidth = 80` включает VHT80. |

## CLI-утилита `lattice-hotspot-switch`

В систему устанавливается утилита командной строки:

```bash
# Текущее состояние (client или ap)
lattice-hotspot-switch mode

# Проверка, активен ли проводной аплинк
lattice-hotspot-switch is-uplink-active

# Подробная диагностика состояния интерфейсов, маршрутов и сервисов
lattice-hotspot-switch status

# Принудительная оценка и переключение режимов
lattice-hotspot-switch eval
```
