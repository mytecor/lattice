# Принять conversation-to-result path

Фича: [F20 — planning loop](./README.md). Зависит от f20-01 и F19.

## Что сделать

- [ ] Провести conversation до согласованной постановки и task commit.
- [ ] Проверить автоматическое обнаружение, выполнение, verification и terminal result commit.
- [ ] Перезапустить planning agent после commit и доказать независимость execution pipeline от его
      session.

## Критерий готовности

- [ ] Полный путь `conversation → Git → execution → Git result` воспроизводим.

## Затрагиваемые файлы / слои

- end-to-end tests/runbook
- roadmap status
