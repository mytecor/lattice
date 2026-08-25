# Текущие расхождения с документацией

Узкие места, которые уже видны и тормозят конкретно F1–F3. Часть из них решается внутри задач
выше, часть — быстрые фиксы:

- `modules/README.md` и `profiles/README.md` описывают каждый каталог как самостоятельный flake с
  `nixosModule`, но на диске лежат только `config.nix`/`options.nix`, а `nodes/example` подключает
  их как `flake = false`. См. [f1-02](../tasks/f1-02-unify-module-profile-model.md) и
  [BUILD_MODEL.md](../../../BUILD_MODEL.md) — решение №1 согласовано.
- `profiles/base` упомянут в README и DEPLOYMENT.md, но не существует — создаётся в
  [f1-01](../tasks/f1-01-assemble-base-profile.md).
- `profiles/rns-server` поднимает только `AutoInterface` со `discovery_scope = "link"`, то есть
  работает в одном broadcast-домене — расширяется в [f2-01](../tasks/f2-01-reticulum-tcp-interfaces.md).
- В `nodes/example/flake.nix` секция profiles закомментирована — включается в
  [f1-01](../tasks/f1-01-assemble-base-profile.md).
- Секреты-заглушки (`dummy-key-for-vm`, `fakeWirelessSecret`) — заменяются в
  [f3-04](../tasks/f3-04-replace-dummy-secrets.md).
