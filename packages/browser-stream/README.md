# Browser stream

Страница просмотра Camoufox через [PhantomStream](https://github.com/fullselfbrowsing/PhantomStream)
0.2.1. Источник — существующий Foxbridge CDP, используемый браузерным агентом.

Пример адреса (значение `url` кодируется через `URLSearchParams` / `encodeURIComponent`):

```text
http://browser.mytecor-homelab.local/?url=https%3A%2F%2Fexample.org%2F%3Fq%3Da%26lang%3Dru
```

`url` принимает абсолютный HTTP(S) адрес без credentials. Сервис выбирает вкладку с точно
совпадающим URL или создаёт новую. Несколько зрителей одной вкладки используют общий capture.
При переходе исходной страницы стрим продолжается, а query-параметр ссылки обновляется.
Новая вкладка закрывается через 15 секунд после отключения последнего зрителя; существующие
вкладки агента сохраняются. Одновременно разрешено до восьми стримов.

Стрим передаёт DOM snapshot и MutationObserver diffs в sandbox iframe. Это режим просмотра:
команды удалённого управления отвергаются на сервере. Canvas, WebGL и защищённые медиа могут
не отображаться как в исходном браузере. Изображения загружаются браузером зрителя по правилам
PhantomStream (по умолчанию HTTPS, без private origins); это не видеопоток.

Foxbridge не реализует `Runtime.addBinding`: сервис использует готовый capture-скрипт
PhantomStream и ограниченную очередь в странице, которую опрашивает через `Runtime.evaluate`.
При переполнении очереди запрашивается новый snapshot; при переходе страницы capture
внедряется заново. Изменения самого `body` / `html` требуют нового snapshot, поскольку
upstream не назначает ID этим оболочкам. Потеря CDP или закрытие вкладки завершает соединение с видимым статусом.
Для повторного подключения после восстановления откройте ссылку заново.

Сборка [package.nix](./package.nix) использует hash-pinned npm-архивы PhantomStream и `ws`;
оба пакета не требуют дополнительных runtime-зависимостей. Локальные проверки:

```sh
cd packages/browser-stream
npm ci --ignore-scripts
npm test
```

Запуск вне Nix:

```sh
BROWSER_CDP_URL=http://127.0.0.1:9222 BROWSER_STREAM_PORT=9223 npm start
```

Backend всегда слушает loopback. Публикация и Authentik описаны в
[профиле](./../../profiles/browser-stream/README.md).
