# Выбрать и реализовать границу изоляции worker

Фича: [F10 — disposable worker](./README.md). Зависит от f10-01.

## Контекст

Pi не является sandbox. Unattended task получает отдельную вычислительную границу с явными CPU,
memory, disk, network и filesystem правами.

## Что сделать

- [ ] Зафиксировать threat model и сравнить VM/microVM/container по обязательным ограничениям.
- [ ] Выбрать backend и описать worker image декларативно в Nix.
- [ ] Ограничить ресурсы, сеть, mounts, devices и host control sockets.
- [ ] Проверить невозможность чтения host secrets/cache directories вне выданных endpoints.

## Критерий готовности

- [ ] Worker создаётся из закреплённого image/config и соблюдает проверяемые resource boundaries.
- [ ] Escape/secret-access negative checks не дают доступ к host control и provider credentials.

## Затрагиваемые файлы / слои

- worker image/module/profile
- security checks
- `ARCHITECTURE.md`

## Открытые вопросы

Backend изоляции — открытое решение №3 в [BACKLOG.md](../BACKLOG.md).
