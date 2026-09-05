# Зафиксировать общий TUI/RPC контракт Pi

Фича: [F8 — интерактивный Pi runtime](../features/f8-pi-runtime.md). Зависит от f8-04.

## Контекст

Будущий worker использует тот же runtime через Pi RPC. До реализации worker нужно проверить, что
TUI-specific state не является скрытым входом выполнения.

## Что сделать

- [ ] Описать RPC entry point, request/result framing, streaming events и exit semantics.
- [ ] Сопоставить model/tool/workspace config TUI и RPC режимов.
- [ ] Выполнить RPC smoke task без интерактивной сессии.
- [ ] Зафиксировать версию контракта и поведение несовместимых версий.

## Критерий готовности

- [ ] Один Pi package/config выполняет эквивалентный smoke task через TUI и RPC.
- [ ] RPC не требует сохранённой Pi session или provider-specific параметров.

## Затрагиваемые файлы / слои

- `profiles/pi/`
- `checks/`
- `ARCHITECTURE.md`

## Открытые вопросы

_нет_.
