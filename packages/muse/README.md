# Muse Code

[Пакет](./package.nix) устанавливает официальный статический Linux-бинарник
[Meta Muse Code](https://dev.meta.ai/docs/muse-code) версии `1.4.11-R6667.1`.
Поддерживаются `x86_64-linux` и `aarch64-linux`; URL и SHA-256 закреплены
отдельно для каждой архитектуры по upstream release manifest.

```sh
nix build .#muse
./result/bin/muse --version
```

Пакет экспортируется как `pkgs.lattice.muse`. Upstream installer и
самообновляющийся launcher не запускаются: обновление версии выполняется через Nix.
Бинарник сохраняется без strip/ELF-патчей; wrapper задаёт CA bundle по умолчанию.
Авторизация и сессии остаются вне Nix store.

Для OpenAI-шлюза используется [Muse ACP](./../muse-acp/README.md).
