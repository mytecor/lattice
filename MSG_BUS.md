# 🕸️ Reticulum Mesh Bus

**Децентрализованная шина сообщений (Pub/Sub) с поддержкой Multi-Hop для Mesh-сетей на базе [Reticulum Network Stack (RNS)](https://reticulum.network/)**

Этот проект демонстрирует, как построить надежную, самоорганизующуюся шину обмена сообщениями без использования центральных брокеров (типа NATS, RabbitMQ или MQTT) и без "заворачивания" тяжеловесного TCP-трафика в сеть Reticulum.

## 🤔 Проблема
Классический подход к объединению узлов — это проброс TCP-портов (например, сервера NATS) через утилиты вроде `rnsh` и `socat`. Но в реалиях Mesh-сетей и медленных каналов (LoRa, пакетное радио) это приводит к критическим проблемам:
1. **TCP-оверхед:** Reticulum шифрует и фрагментирует TCP-пакеты, что создает огромный избыточный трафик (служебные пинги брокера забивают канал).
2. **Ограничения групп:** Встроенные в RNS групповые рассылки (`Destination.GROUP`) отлично работают в локальной сети, но на данный момент **не поддерживают Multi-hop** маршрутизацию через промежуточные узлы.

## 💡 Решение
Мы используем **нативный механизм Анонсов (Announces)** Reticulum и динамический сбор узлов. 

Каждый узел запускает обработчик `AnnounceHandler`, который фильтрует сеть и автоматически находит другие узлы с таким же именем приложения (`app_name`). При отправке сообщения в шину, узел просто рассылает легковесные нативные пакеты (`Destination.SINGLE`) всем известным участникам.

### ✨ Преимущества
* **Автообнаружение (Auto-Discovery):** Узлам не нужно знать IP-адреса или хэши друг друга заранее. Сеть сама находит новые устройства.
* **Идеальный Multi-Hop:** Пакеты маршрутизируются через любое количество промежуточных ретрансляторов (идеально для LoRa-сетей).
* **Никаких брокеров:** Полная децентрализация (P2P). Если часть сети отпадет, оставшиеся узлы продолжат общаться.
* **Безопасность:** Сквозное шифрование (E2E) с прямой секретностью (Perfect Forward Secrecy) для каждого пакета.
* **Минимальный вес:** Никакого TCP-рукопожатия, отправляется только полезная нагрузка + криптографическая подпись RNS.

---

## 💻 Пример кода (`mesh_bus.py`)

```python
import RNS
import time

APP_NAME = "mesh_bus"
ASPECT = "node"

class BusAnnounceHandler:
    """Обработчик для автоматического поиска узлов нашей шины"""
    def __init__(self, aspect_filter):
        self.aspect_filter = aspect_filter
        self.known_nodes = {}

    def received_announce(self, destination_hash, announced_identity, app_data):
        hex_hash = RNS.prettyhexrep(destination_hash)
        
        if destination_hash not in self.known_nodes:
            print(f"[DISCOVERY] Найдена новая нода в нашей группе: {hex_hash}")
        else:
            print(f"[DISCOVERY] Обновлен маршрут до ноды: {hex_hash}")
            
        # Сохраняем публичный ключ узла для будущей связи
        self.known_nodes[destination_hash] = announced_identity

# 1. Инициализируем сеть
reticulum = RNS.Reticulum()
my_identity = RNS.Identity()

# 2. Создаем нашу точку входа (слушатель)
my_destination = RNS.Destination(
    my_identity, 
    RNS.Destination.IN, 
    RNS.Destination.SINGLE, 
    APP_NAME, 
    ASPECT
)

def on_message_received(data, packet):
    print(f"[ВХОДЯЩЕЕ СООБЩЕНИЕ]: {data.decode('utf-8')}")

my_destination.set_packet_callback(on_message_received)

# 3. Регистрируем обработчик для поиска других узлов
announce_filter = f"{APP_NAME}.{ASPECT}"
handler = BusAnnounceHandler(aspect_filter=announce_filter)
RNS.Transport.register_announce_handler(handler)

# 4. Анонсируем себя в сеть, чтобы другие нас нашли
my_destination.announce()
print(f"[*] Шина запущена. Мой хэш: {RNS.prettyhexrep(my_destination.hash)}")

# 5. Функция публикации в шину
def publish_to_bus(message_str):
    if not handler.known_nodes:
        return # Нет известных узлов

    data = message_str.encode('utf-8')
    count = 0
    
    for dest_hash, peer_identity in handler.known_nodes.items():
        peer_dest = RNS.Destination(
            peer_identity,
            RNS.Destination.OUT,
            RNS.Destination.SINGLE,
            APP_NAME,
            ASPECT
        )
        packet = RNS.Packet(peer_dest, data)
        packet.send()
        count += 1
        
    print(f"[ШИНА] Отправлено {count} узлам.")

# --- Демонстрационный цикл ---
try:
    while True:
        time.sleep(15)
        publish_to_bus('{"event": "ping", "status": "ok"}')
        # В продакшене не забывайте периодически вызывать my_destination.announce()
        # например, раз в час, чтобы новые устройства могли вас обнаружить.
except KeyboardInterrupt:
    print("Выход...")
```

---

## 🛠️ Как интегрировать это в свой проект

Вы можете легко адаптировать этот шаблон под свои задачи:
1. Замените функцию `publish_to_bus()` на обработчик ваших собственных событий (показания датчиков, команды умного дома, системные логи).
2. Используйте сериализацию (например, JSON или MessagePack) для передачи сложных данных в `message_str`. *Помните, что максимальный размер одного пакета в Reticulum ограничен (~430 байт).*
3. Измените `APP_NAME` и `ASPECT` на уникальные для вашего проекта (например, `myapp`, `sensors`), чтобы изолировать трафик от других приложений в сети.

## 🔒 Безопасность и приватность
Данная шина использует весь криптографический потенциал Reticulum. Если вы хотите скрыть само существование вашей шины от публичной сети Testnet, используйте механизм **Interface Access Codes (IFAC)**. Для этого добавьте параметры `network_name` и `passphrase` в настройки ваших интерфейсов в конфигурационном файле `~/.reticulum/config`. Это полностью изолирует ваш трафик на криптографическом уровне.
