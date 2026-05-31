# Lattice GitOps Deploy Module

Модуль включает pull-based GitOps-деплой для нод Lattice через [`nlewo/comin`](https://github.com/nlewo/comin).

По умолчанию агент деплоя опрашивает `https://github.com/mytecor/lattice.git`, ветку `main`, и разворачивает `nixosConfigurations.<hostname>` из корневого flake репозитория.

Имя конфигурации выбирает сам `comin` через `services.comin.hostname`; при необходимости нода может задать его явно.

Нода может переопределить настройки через стандартные опции `services.comin.*`, например `services.comin.remotes` или `services.comin.hostname`.
