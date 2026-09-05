# F9. Cache и artifact plane

Долгоживущие ускорители отделяются от ценных результатов. Git mirrors, npm packages и Nix
nar-файлы являются disposable caches на локальной POSIX FS; artifacts, screenshots, logs, builds,
backups и test outputs публикуются как объектные данные с явной ссылкой из результата задачи.

Зависит от source bootstrap [f4-01](../tasks/f4-01-radicle-seed-comin.md),
[F7](./f7-llm-gateway.md) и [F8](./f8-pi-runtime.md). Соответствует
[вехе 9](../VISION.md#вехи-и-зависимости-без-деталей).

Задачи: [f9-01](../tasks/f9-01-git-cache-proxy.md),
[f9-02](../tasks/f9-02-git-repository-access.md),
[f9-03](../tasks/f9-03-verdaccio.md),
[f9-04](../tasks/f9-04-attic.md),
[f9-05](../tasks/f9-05-artifact-contract.md),
[f9-06](../tasks/f9-06-cache-loss-drill.md).

**Критерий готовности:** Pi получает ускорение Git/npm/Nix из локальных caches, артефакт
публикуется и читается по immutable reference, а удаление любого cache влияет только на время
следующего выполнения. Private Git objects не выдаются клиенту без repo-scoped authorization.

**Не входит:** shared POSIX filesystem между workers и S3-FUSE. Выбор конкретного S3-compatible
хранилища не должен менять artifact contract.
