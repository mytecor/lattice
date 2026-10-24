# Camoufox

[Пакет](./package.nix) закрепляет Firefox-based Camoufox `152.0.4-beta.30`.
Официальный release zip адаптируется к NixOS через autoPatchelf с сохранением
relrhack sections (`--no-clobber-old-sections`).

FFmpeg 8 (libavcodec 62), libglvnd и Mesa добавляются в RUNPATH всех shared
objects, включая `libxul.so`. Firefox загружает декодер через `dlopen` из
shared library: RUNPATH только на launcher не наследуется таким вызовом.
Версия FFmpeg закреплена явно, чтобы обновление alias не сменило ABI на
неподдерживаемый Firefox 152.

В конфигурацию браузера добавлены defaults `webgl.force-enabled` и
`webgl.enable-webgl2`, которые обычно задаёт Python launcher Camoufox.
В [профиле стека](./../../profiles/browser-agent-stack/README.md)
браузер запускается в headless-режиме с доступом к GPU (`/dev/dri/renderD128`)
через Headless EGL и VA-API. CSP и same-origin policy не отключаются.

Runtime-проверка на homelab: декодирование GitHub H.264 MP4 до видеокадра
`2496×1416` и WebGL clear/readPixels с ожидаемым RGBA `[0,255,0,255]`.
Настройка сервиса описана в [модуле](./../../modules/services/foxbridge-camoufox/README.md).

[Патч Juggler](./devtools-input.patch) пропускает повторные точки humanize,
которые Gecko объединяет без renderer ack, измеряет browser rect после flush
композитора и ограничивает ожидание ack с очисткой watcher. Потерянное
подтверждение возвращает ошибку и освобождает общую очередь мыши и колеса.
Подписка на APZ flush создаётся до запроса; ожидание ограничено, observer очищается.
CSS-геометрия и scroll offsets читаются в content-процессе, поскольку размеры
browser element в chrome-процессе могут отличаться от координат DOM.
Screencast дополнительно передаёт CSS viewport независимо от размера JPEG.
[Проверка ввода](./input.test.cjs) исполняет обработчик из пропатченного архива
с моделированием повторной точки и потерянного ack при каждой сборке пакета.

Screencast захватывает видимый viewport через `drawSnapshot(null, ...)`, а не
прямоугольник документа с началом `(0, 0)`: при прокрутке последний создаёт
ложное движение картинки в обратную сторону и пустое пространство сверху.
[Проверка захвата](./screencast.test.cjs) проверяет viewport capture, масштаб,
фиксированную геометрию кадра и scroll metadata на разных позициях страницы.
