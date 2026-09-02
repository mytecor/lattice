# Текущие расхождения с документацией

Узкие места, которые уже видны и тормозят конкретно F1–F3. Часть из них решается внутри задач
выше, часть — быстрые фиксы:

- `profiles/rns-server` поднимает только `AutoInterface` со `discovery_scope = "link"`, то есть
  работает в одном broadcast-домене — расширяется в [f3-01](../tasks/f3-01-reticulum-tcp-interfaces.md).
- Секреты-заглушки (`dummy-key-for-vm`, `fakeWirelessSecret`) — заменяются через `agenix` в
  [f2-04](../tasks/f2-04-replace-dummy-secrets.md).
