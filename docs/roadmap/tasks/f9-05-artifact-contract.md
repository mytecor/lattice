# Определить контракт artifacts в object storage

Фича: [F9 — cache и artifact plane](../features/f9-cache-artifact-plane.md).

## Контекст

Screenshots, logs, builds, backups и test outputs естественно объектные. Workers не монтируют S3
как рабочую filesystem; результат задачи содержит явные immutable references.

## Что сделать

- [ ] Определить artifact manifest: task/run id, media type, size, digest, location и provenance.
- [ ] Определить immutable object keys, integrity check, retention и garbage-collection boundary.
- [ ] Разделить public/private artifacts и выдачу минимальных upload/download credentials.
- [ ] Реализовать round-trip sample artifact без S3-FUSE.

## Критерий готовности

- [ ] Artifact проверяется по digest и восстанавливается только по manifest/reference.
- [ ] Замена S3-compatible backend не меняет task/result contract.

## Затрагиваемые файлы / слои

- schema/contracts для artifacts
- `profiles/artifact-store/`
- `ARCHITECTURE.md`

## Открытые вопросы

Конкретный S3-compatible backend выбирается при реализации, а не включается в контракт.
