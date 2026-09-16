{ config, lib, pkgs, ... }:

let
  hostName = config.networking.hostName;
  statusHost = "status.${hostName}.local";

  # f4-04: JSON генерируется на каждой активации из runtime-фактов узла
  # (NixOS generation, применённый comin commit). Обслуживает сам Caddy через
  # file_server — без отдельного backend-процесса и внутреннего порта
  # (контракт f4-02 сохранён).
  statusFile = "/run/lattice-node-status.json";
  cominSourceRepo = "/var/lib/comin/source/repository";
  # f4-04: writer статус-документа. Собираем через replaceVarsWith: вшиваем
  # полные store-пути bash/git/jq (@bash@/@git@/@jq@) — их нет в активационной
  # среде NixOS (иначе activation падал 127 и валил comin-switch). Остальные
  # команды (readlink/basename/hostname/uname/date/chmod/mv) уже есть в PATH
  # активации через coreutils. dir="bin" + isExecutable дают executable в
  # $out/bin/lattice-node-status-write.
  statusWriterPkg = pkgs.replaceVarsWith {
    name = "lattice-node-status-write";
    src = ./status-write.sh;
    replacements = {
      bash = "${pkgs.bash}/bin/bash";
      git = "${pkgs.git}/bin/git";
      jq = "${pkgs.jq}/bin/jq";
      hostname = "${pkgs.inetutils}/bin/hostname";
    };
    dir = "bin";
    isExecutable = true;
  };
  statusWriter = "${statusWriterPkg}/bin/lattice-node-status-write";
in
{
  imports = [ ../tcp-gateway/config.nix ];

  config = {
    services.caddy.virtualHosts."http://${statusHost}".extraConfig = ''
      root * ${statusFile}
      header Content-Type application/json
      file_server
    '';

    # f4-04: пишем статус-документ при каждой активации. Специальный
    # 'lattice-node-status' activation script работает без отдельного юнита:
    # значения generation/commit меняются именно на активации, а Caddy читает
    # файл только по запросу. Если коммит ещё не выбран (свежая нода до первого
    # comin-цикла), commit=null, endpoint остаётся валидным JSON.
    system.activationScripts.lattice-node-status = {
      deps = [ ];
      text = ''
        LATTICE_NODE_STATUS_FILE=${statusFile} \
        LATTICE_NODE_STATE_VERSION=${lib.escapeShellArg config.system.stateVersion} \
        LATTICE_COMIN_SOURCE_REPO=${lib.escapeShellArg cominSourceRepo} \
        ${statusWriter}
      '';
    };

    systemd.services.node-status-mdns = {
      description = "Publish the node status mDNS alias";
      wantedBy = [ "multi-user.target" ];
      after = [ "avahi-daemon.service" "network-online.target" ];
      requires = [ "avahi-daemon.service" ];
      wants = [ "network-online.target" ];
      script = ''
        address="$(${pkgs.iproute2}/bin/ip -4 -o route get 1.1.1.1 \
          | ${pkgs.gawk}/bin/awk '{ for (i = 1; i <= NF; i++) if ($i == "src") { print $(i + 1); exit } }')"
        if [ -z "$address" ]; then
          echo "could not determine the primary IPv4 address" >&2
          exit 1
        fi
        exec ${config.services.avahi.package}/bin/avahi-publish \
          --address --no-reverse ${lib.escapeShellArg statusHost} "$address"
      '';
      serviceConfig = {
        Restart = "always";
        RestartSec = 5;
      };
    };
  };
}
