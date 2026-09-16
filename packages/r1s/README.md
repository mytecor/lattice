# r1s package

Собирает [r1s](https://github.com/mytecor/r1s) — OCI workload execution fabric над RNS — из
закреплённого GitHub commit как `buildGoModule`. Файл [`package.nix`](./package.nix) — обычная
функция `{ lib, go, buildGoModule, fetchFromGitHub }`, подключаемая через `final.callPackage`.

- Сборка даёт оба бинарника в одном store-path: `r1s` (клиент) и `r1sd` (allocator над containerd).
  Оба выведены в `packages.${system}`: `r1s`, а `r1sd` — алиас на тот же пакет.
- Требует `go >= 1.27.1` (go.mod + Reticulum-Go v1.2.0). Override `go_1_27` делает overlay flake
  (`final.go_1_27`, 1.27.1 в основном пине nixpkgs).
- `doCheck = true` гоняет `go test ./...` в sandbox; runtime-интеграция с containerd замыкается
  через интерфейсы, живого containerd не нужно.

## Проверка

```sh
nix flake check            # eval всех outputs
nix build .#packages.x86_64-linux.r1s   # на x86_64-linux билдере/ноде или в GitHub Actions
```
