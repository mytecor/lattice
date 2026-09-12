{ config, lib, pkgs, ... }:

let
  hostName = config.networking.hostName;
  serviceHost = service: "${service}.${hostName}.local";
  siteAddress = service: "http://${serviceHost service}";

  radicleEnabled =
    (config.services.radicle.enable or false) && (config.services.radicle.httpd.enable or false);
  rnsServerEnabled =
    (config.lattice.rns-server.enable or false) &&
    (config.lattice.rns-server.server.http.enabled or false) &&
    (config.lattice.rns-server.server.http.port or null != null);
  llmGatewayEnabled = config.lattice.llm-gateway.enable or false;
  piAcpEnabled = config.lattice.pi-acp-daemon.enable or false;
  piAcpCfg = config.lattice.pi-acp-daemon;
  gitCacheProxyEnabled = config.lattice.git-cache-proxy.enable or false;
  gitCacheProxyCfg = config.lattice.git-cache-proxy;

  mdnsPublisher = service: {
    description = "Publish the ${service} mDNS alias";
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
        --address --no-reverse ${lib.escapeShellArg (serviceHost service)} "$address"
    '';
    serviceConfig = {
      Restart = "always";
      RestartSec = 5;
    };
  };

  # Автоматически собираем проксирование для активных TCP/HTTP сервисов
  proxiedServices = lib.mkMerge [
    # 1. Radicle HTTP Gateway
    (lib.mkIf radicleEnabled {
      ${siteAddress "radicle"} = {
        extraConfig = ''
          reverse_proxy 127.0.0.1:${toString config.services.radicle.httpd.listenPort}
        '';
      };
    })

    # 2. RNS Server HTTP Control Plane
    (lib.mkIf rnsServerEnabled {
      ${siteAddress "rns-server"} = {
        extraConfig = ''
          reverse_proxy 127.0.0.1:${toString config.lattice.rns-server.server.http.port}
        '';
      };
    })

    # 3. OpenAI-compatible LLM Gateway via its mDNS alias.
    (lib.mkIf llmGatewayEnabled {
      ${siteAddress "llm-gateway"} = {
        extraConfig = ''
          reverse_proxy ${config.lattice.llm-gateway.host}:${toString config.lattice.llm-gateway.port}
        '';
      };
    })

    # 4. Authenticated LAN WebSocket ingress for the loopback-only Pi ACP daemon.
    (lib.mkIf piAcpEnabled {
      ${siteAddress piAcpCfg.serviceName} = {
        extraConfig = ''
          handle {
            rewrite * /acp?token=${piAcpCfg.internalToken}
            reverse_proxy ${piAcpCfg.host}:${toString piAcpCfg.port} {
              header_up -Authorization
            }
          }
        '';
      };
    })

    # 5. LAN ingress for the loopback-only Git cache proxy (F9).
    # The proxy is a shared credentialed reader; "can reach this host" ==
    # "can read every mirrored repo", so it is exposed only through the
    # operator-controlled Caddy ingress on the LAN.
    (lib.mkIf gitCacheProxyEnabled {
      ${siteAddress "git-cache-proxy"} = {
        extraConfig = ''
          reverse_proxy ${gitCacheProxyCfg.host}:${toString gitCacheProxyCfg.port}
        '';
      };
    })
  ];
in
{
  config = {
    services.caddy = {
      enable = true;
      virtualHosts = proxiedServices;
    };

    networking.firewall.allowedTCPPorts = [ 80 ];

    services.avahi = {
      enable = true;
      publish = {
        enable = true;
        addresses = true;
        userServices = true;
      };
    };

    # Avahi publishes only the node's primary hostname by default. Keep one
    # publisher per active service so every Caddy host resolves through mDNS.
    systemd.services = lib.mkMerge [
      (lib.mkIf radicleEnabled { radicle-mdns = mdnsPublisher "radicle"; })
      (lib.mkIf rnsServerEnabled { rns-server-mdns = mdnsPublisher "rns-server"; })
      (lib.mkIf llmGatewayEnabled { llm-gateway-mdns = mdnsPublisher "llm-gateway"; })
      (lib.mkIf piAcpEnabled {
        "${piAcpCfg.serviceName}-mdns" = mdnsPublisher piAcpCfg.serviceName;
      })
      (lib.mkIf gitCacheProxyEnabled {
        git-cache-proxy-mdns = mdnsPublisher "git-cache-proxy";
      })
    ];
  };
}
