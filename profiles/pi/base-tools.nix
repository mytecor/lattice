# Базовый контракт tools для Pi-рантайма (f8-03).
#
# Единая точка, из которой собирается и системный профиль (NixOS), и devShell
# разработчика: интерактивная нода и будущий worker должны получать один и тот же
# базовый набор bash/git/tools, а проектные зависимости — добавляться сверху из
# project flake/devShell, не меняя рантайм Pi.
#
# Набор — минимальный: оболочка, git, стандартные shell-инструменты, сетевые
# клиенты и пара редакторов/поиска. Тяжёлый project-specific toolchain
# (node, go, rust…) в базу не входит — проект подключает его собственным
# devShell / `lattice.pi.tools`.
{ pkgs }:

let
  base = [
    pkgs.bashInteractive
    pkgs.coreutils
    pkgs.curl
    pkgs.diffutils
    pkgs.file
    pkgs.findutils
    pkgs.gawk
    pkgs.git
    pkgs.gnugrep
    pkgs.gnused
    pkgs.gnutar
    pkgs.gzip
    pkgs.jq
    pkgs.openssh
    pkgs.procps
    pkgs.ripgrep
    pkgs.tree
    pkgs.vim
    pkgs.which

    # Дополнительно к `git`: подпись/шифрование через GnuPG.
    pkgs.gnupg
    # `xxd` для hex-работы (присутствует и в coreutils-сборках, закрепляем явно).
    pkgs.xxd
  ];
in
{
  inherit base;

  # Декларативное расширение: проект добавляет свой toolchain к базовому
  # контракту, не трогая рантайм Pi.
  #
  #     let tools = (import "${lattice}/profiles/pi/base-tools.nix" { inherit pkgs; });
  #     in tools.compose [ pkgs.nodejs pkgs.go ]
  compose = extra: base ++ extra;
}
