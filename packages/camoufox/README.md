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
Для WebGL без GPU [профиль стека](./../../profiles/browser-agent-stack/README.md)
запускает headed-браузер под Xvfb с программным Mesa; сам нативный headless
не обеспечивает этот контракт. CSP и same-origin policy не отключаются.

Runtime-проверка на homelab: декодирование GitHub H.264 MP4 до видеокадра
`2496×1416` и WebGL clear/readPixels с ожидаемым RGBA `[0,255,0,255]`.
Настройка сервиса описана в [модуле](./../../modules/services/foxbridge-camoufox/README.md).
