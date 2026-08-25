# Модель сборки: разбор вариантов

Рабочий документ для решения №1 из [ROADMAP.md](./ROADMAP.md). После выбора варианта его итог
переезжает в [ARCHITECTURE.md](./ARCHITECTURE.md), а этот файл удаляется.

> **Решение №1 (согласовано 25 авг.):** модель сборки = **`flake = false`**, модули приходят в
> inputs, а не импортами.
>
> **Состав решения:**
> - **Каждая нода — самостоятельный flake** (свой `flake.nix`/`flake.lock`), как сейчас
>   `nodes/example`. Модули подключаются как inputs с `flake = false` (без собственных
>   `flake.nix`/`flake.lock`; в lock ноды — path-type записи).
> - Ноды **могут** иметь разные версии nixpkgs — осознанный выбор ради разного хардвара/пакетов;
>   единый сетевой lock не вводится. Расхождение версий возможно и это принимается.
> - Зависимости модулей — **типизированные опции**, а не скрытые аргументы:
>   * пакеты (`rns-server`, `rnsh`) — опция `types.package` с дефолтом `pkgs.lattice.*` через overlay,
>     аргументы `rnsServerPackage`/`rnshPackage` убираются;
>   * порты — типизированные опции (`types.port`/`types.int`), которые **собирает нода** и передаёт
>     модулям; аргумент `latticePorts` убирается.
> - Реестр `profiles/networking/ports.nix` сохраняется как источник значений, при этом нода может
>   переопределить любой порт вручную.
> - Отчуждаемость — на уровне узла (модули используются во внешних репо нод) без цикла зависимостей;
>   это исключает вариант C (там цикл корень↔нода). B не нужен: отдельный flake на модуль сверх меры.
> - `profiles/base` = gitops + unfree-предикат + базовые системные дефолты.
>
> **Напоминание:** реализация этапа 1 намечена на отдельную сессию. До неё не начинать миграцию.

## Что нужно решить

Сейчас `modules/README.md` и `profiles/README.md` требуют, чтобы каждый каталог был самостоятельным
flake с output `nixosModule`. На диске лежат обычные модули: `config.nix`, `options.nix`, иногда
`default.nix`. Единственный настоящий flake среди слоев - `packages/rns-rs`. `nodes/example`
подключает модули как `flake = false` пути, то есть работает по третьей, нигде не описанной модели.

Вместе с этим нужно решить, как модули получают свои зависимости. Сегодня контракт неявный:

- `modules/rns-server/options.nix` ждет аргумент `rnsServerPackage`;
- `modules/rnsh/options.nix` ждет `rnshPackage`;
- `profiles/{rns-server,radicle,networking}` ждут `latticePorts`;
- `nodes/example/flake.nix` не передает ничего из этого.

Плюс скрытая деталь: `packages/rns-rs/flake.nix` объявляет `allowUnfreePredicate` для `rns-server` и
`rnsh` внутри себя. Как только пакеты начнут собираться через оверлей в контексте узла, это условие
нужно будет объявить на уровне узла, иначе сборка упрется в unfree-лицензию.

## Вариант A. Один flake в корне

### Дерево

```
flake.nix                     # единственный flake: inputs, overlay, nixosModules, nixosConfigurations
flake.lock                    # единственный lock
hardware/intel-n100/default.nix
modules/rns-server/{default,options,options-server,options-rnsd,config}.nix
profiles/base/default.nix
profiles/rns-server/default.nix
nodes/example/{default.nix,disko.nix,secrets/,files/}
packages/rns-rs/package.nix   # без своего flake.nix, вызывается через callPackage
```

### flake.nix

```nix
{
  description = "Lattice network";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    disko.url = "github:nix-community/disko";
    disko.inputs.nixpkgs.follows = "nixpkgs";
    impermanence.url = "github:nix-community/impermanence";
    nixos-hardware.url = "github:NixOS/nixos-hardware";
  };

  outputs = { self, nixpkgs, ... }@inputs:
    let
      inherit (nixpkgs) lib;
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = f: lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      overlay = final: _prev: {
        lattice = {
          rns-server = final.callPackage ./packages/rns-rs/package.nix { bin = "rns-server"; };
          rnsh = final.callPackage ./packages/rns-rs/package.nix { bin = "rnsh"; };
        };
      };

      mkNode = { name, system ? "x86_64-linux", modules ? [ ] }:
        lib.nixosSystem {
          specialArgs = { inherit inputs; };
          modules = [
            { nixpkgs.hostPlatform = system; nixpkgs.overlays = [ overlay ]; }
            self.nixosModules.default
            ./profiles/base
            ./nodes/${name}
          ] ++ modules;
        };
    in
    {
      overlays.default = overlay;

      packages = forAllSystems (pkgs: {
        rns-server = pkgs.callPackage ./packages/rns-rs/package.nix { bin = "rns-server"; };
        rnsh = pkgs.callPackage ./packages/rns-rs/package.nix { bin = "rnsh"; };
      });

      nixosModules = {
        rns-server = ./modules/rns-server;
        rnsh = ./modules/rnsh;
        ephemeral-root = ./modules/ephemeral-root;
        wireless = ./modules/wireless;
        default.imports = [
          ./modules/rns-server
          ./modules/rnsh
          ./modules/ephemeral-root
          ./modules/wireless
        ];
      };

      nixosConfigurations.example = mkNode {
        name = "example";
        modules = [ ./profiles/rns-server ./profiles/rnsh ./profiles/gitops ];
      };
    };
}
```

`nixosModules.default` подключает только опции: пока `enable = false`, ничего в систему не попадает.
Роль включают профили.

### Что меняется в модулях

Скрытые аргументы исчезают. В `modules/rns-server/options.nix`:

```nix
package = mkOption {
  type = types.package;
  default = pkgs.lattice.rns-server;
  defaultText = lib.literalExpression "pkgs.lattice.rns-server";
};
```

Реестр портов перестает быть аргументом модуля и становится обычным импортом там, где он нужен:

```nix
{ ... }:
let ports = import ../networking/ports.nix;
in { config.services.radicle.node.listenPort = ports.radicle-node; }
```

Unfree-предикат переезжает в `profiles/base`:

```nix
nixpkgs.config.allowUnfreePredicate = pkg:
  builtins.elem (lib.getName pkg) [ "rns-server" "rnsh" ];
```

### Внешние ноды

Зависимость направлена в одну сторону, поэтому внешняя нода живет в своем репозитории:

```nix
# репозиторий чужой ноды
{
  inputs.lattice.url = "github:mytecor/lattice";
  outputs = { nixpkgs, lattice, ... }: {
    nixosConfigurations.remote-node = nixpkgs.lib.nixosSystem {
      modules = [
        { nixpkgs.overlays = [ lattice.overlays.default ]; }
        lattice.nixosModules.default
        lattice.profiles.base            # если экспортировать профили отдельным output
        ./config.nix
      ];
    };
  };
}
```

Ее `comin` смотрит на ее собственный репозиторий, а не на наш. Наш корневой flake про нее вообще
ничего не знает - и это то, что нужно.

### Цена

- Все ноды сети делят один `nixpkgs` и один `flake.lock`. Обновление nixpkgs - событие для всей сети
  сразу, откатывать тоже придется всем сразу.
- Граница между слоями держится дисциплиной и code review, а не механикой flake.

## Вариант B. Flake в каждом каталоге

### Дерево

```
flake.nix + flake.lock
modules/rns-server/flake.nix + flake.lock + *.nix
modules/rnsh/flake.nix + flake.lock + *.nix
modules/ephemeral-root/flake.nix + flake.lock + *.nix
modules/wireless/flake.nix + flake.lock + *.nix
profiles/base/flake.nix + flake.lock
profiles/gitops/flake.nix + flake.lock
... еще четыре профиля
packages/rns-rs/flake.nix + flake.lock
nodes/example/flake.nix + flake.lock
```

Одиннадцать flake и одиннадцать lock-файлов на текущий состав репозитория.

### Модуль как flake

```nix
# modules/rns-server/flake.nix
{
  inputs.rns-rs.url = "path:../../packages/rns-rs";

  outputs = { rns-rs, ... }: {
    nixosModules.default = { pkgs, ... }: {
      imports = [ ./options.nix ./config.nix ];
      _module.args.rnsServerPackage = rns-rs.packages.${pkgs.stdenv.hostPlatform.system}.rns-server;
    };
  };
}
```

### Три проблемы, которые надо принять вместе с вариантом

1. **Относительные path-инпуты хрупкие.** `path:../../packages/rns-rs` выходит за пределы каталога
   самого flake. Пока весь репозиторий - один git-tree и подфлейки берутся как
   `git+file://...?dir=modules/rns-server`, это работает, потому что в стор копируется весь репозиторий.
   Стоит получить каталог модуля отдельно - и ссылка перестанет разрешаться.
2. **Умножение nixpkgs.** Каждый lock пинит свой `nixpkgs`. Без сквозного `follows` в сборке узла
   окажется несколько разных nixpkgs: дольше eval, больше закачек, возможны конфликты версий одной
   библиотеки в разных сервисах. Сквозной `follows` через относительные пути расписывается вручную и
   в каждом flake заново.
3. **Два коммита на одно изменение.** Правка модуля не видна узлу, пока не обновлен lock того, кто
   его импортирует. При pull-деплое через `comin` это значит: правишь модуль, коммитишь, обновляешь
   lock профиля, коммитишь, обновляешь lock ноды, коммитишь. Забыл шаг - нода тихо осталась на старой
   версии, и это не отличается от штатной работы.

### Что взамен

- Граница слоя проверяется машиной: модуль, который полез в чужой слой, просто не соберется.
- Любой модуль можно отдать наружу отдельно от сети Lattice.
- Каждый слой обновляется независимо, что важнее при большом числе внешних участников.

## Вариант C. Flake только у нод и packages

Корневой flake экспортирует модули и профили как обычные пути; каждая нода остается самостоятельным
flake со своим lock; корень импортирует ноды как inputs - примерно то, что заявлено в README сейчас.

```nix
# flake.nix
{
  inputs.example.url = "path:./nodes/example";
  outputs = inputs: {
    nixosConfigurations.example = inputs.example.nixosConfigurations.example;
  };
}
```

Здесь всплывает неприятная деталь текущей схемы: нода лежит внутри репозитория и импортирует
`../../modules` - то есть корень зависит от ноды через input, а нода от корня через путь. Пока модули
подключаются как `flake = false`, цикла нет, но как только ноде понадобится профиль с зависимостями,
эта конструкция начнет требовать ручной развязки.

Плюс сохраняется проблема двух коммитов: `path:./nodes/example` пинится по narHash, и после правки
ноды нужно `nix flake update example` в корне, иначе `comin` соберет старое состояние.

Вариант имеет смысл, если ноды должны иметь разные версии nixpkgs. Для доверенного круга из
нескольких узлов это скорее минус: разъехавшиеся ноды сложнее чинить.

## Сравнение

| | A: один flake | B: flake везде | C: flake у нод |
|---|---|---|---|
| lock-файлов сейчас | 1 | 11 | 3+ |
| коммитов на правку модуля | 1 | 3 | 2 |
| версий nixpkgs в сети | 1 | много | по числу нод |
| граница слоев | дисциплина | механика | частично |
| нода с другой версией nixpkgs | нет | да | да |
| внешняя нода | своим репо через input | своим репо | своим репо |
| скорость eval | лучшая | худшая | средняя |

## Рекомендация

**Вариант A** до тех пор, пока все ноды свои.

Причины по порядку важности:

1. Деплой pull-based. При `comin` цена забытого lock-обновления - нода молча не обновилась. Вариант A
   делает это состояние невозможным: один коммит в `main` - и все узлы видят одно и то же.
2. Работа рывками. Три коммита на одну правку модуля - это то, что забывается после месяца паузы.
3. Отчуждаемость от этого не страдает. Внешняя нода подключает `lattice` как input и берет
   `nixosModules`/`overlays` - зависимость идет в одну сторону, циклов нет. Именно это и нужно для
   этапа 5 роадмапа.

Вариант B стоит своей цены, только когда модули развиваются разными людьми в разном темпе. Это
ситуация зрелого проекта с внешними контрибьюторами, а не первой железной ноды. Переход A → B
механический: у каждого каталога уже свой набор файлов, добавить `flake.nix` можно позже, не трогая
содержимое модулей.

### Заодно предлагаю закрыть вопрос зависимостей

Скрытые аргументы модулей убрать целиком:

- пакеты - через оверлей: `pkgs.lattice.rns-server`, `pkgs.lattice.rnsh`;
- порты - обычным `import ../networking/ports.nix` в тех профилях, где они нужны;
- unfree-предикат - в `profiles/base`.

После этого любой модуль подключается одной строкой без сопровождающего набора `specialArgs`, и
ошибка "модуль ждет аргумент, которого никто не передал" перестает быть возможной.

## Что дальше

Если вариант A принят, порядок работ:

1. Собрать корневой `flake.nix` по образцу выше, перенести `packages/rns-rs` на `callPackage`.
2. Убрать `rnsServerPackage`, `rnshPackage`, `latticePorts` из аргументов модулей.
3. Написать `profiles/base` и включить в него `gitops`, unfree-предикат и базовые системные дефолты.
4. Перевести `nodes/example` на новый вид: обычный каталог с `default.nix` вместо своего flake.
5. Обновить `modules/README.md`, `profiles/README.md`, `nodes/README.md`, `DEPLOYMENT.md`.
6. Проверить: `nix flake check` и `nixos-rebuild build --flake .#example`.
