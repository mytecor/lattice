{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.observability-alloy;

  # JSON fields to extract from each gateway event line into structured
  # metadata (derived from cfg.structuredMetadataFields so the contract test
  # can assert on the generated file without duplicating the module logic).
  jsonExpressions = lib.concatMapStringsSep "\n"
    (field: "      ${field} = \"\",")
    cfg.structuredMetadataFields;
  metadataEntries = lib.concatMapStringsSep "\n"
    (field: "      ${field} = \"\",")
    cfg.structuredMetadataFields;

  # The Alloy config: read the llm-gateway systemd unit from the journal,
  # keep only a constant `service` label (low cardinality), extract the
  # event's dimensions into structured metadata (searchable, not labels), and
  # push everything to the loopback Loki.
  alloyConfigText = ''
    loki.source.journal "llm_gateway" {
      format_as_json = false
      max_age = "12h"
      matches = "${lib.concatStringsSep " " cfg.journalUnitFilter}"
      forward_to = [ loki.process.llm_gateway.receiver ]
    }

    loki.process "llm_gateway" {
      stage.static_labels {
        values = {
          service = "llm-gateway",
        }
      }
      stage.json {
        expressions = {
    ${jsonExpressions}
        }
      }
      stage.structured_metadata {
        values = {
    ${metadataEntries}
        }
      }
      forward_to = [ loki.write.llm_gateway.receiver ]
    }

    loki.write "llm_gateway" {
      endpoint {
        url = "${cfg.lokiUrl}/loki/api/v1/push"
      }
    }
  '';
in
{
  config = lib.mkIf cfg.enable {
    services.alloy = {
      enable = true;
      package = cfg.package;
      extraFlags = [
        "--server.http.listen-addr=${cfg.listenAddress}:${toString cfg.port}"
        "--disable-reporting"
      ];
    };

    environment.etc."alloy/config.alloy".text = alloyConfigText;

    # f12-03 sandbox: the nixpkgs alloy unit is deliberately thin (DynamicUser
    # + journal group only); the lattice wrapper tightens it to the same trust
    # boundary as the other Lattice services. Alloy only reads the journal and
    # pushes to loopback Loki, so it needs no ambient capability, no write
    # beyond its state directory, and no reachable network beyond loopback.
    # DynamicUser stays enabled (deepest default isolation).
    systemd.services.alloy = {
      serviceConfig = {
        AmbientCapabilities = "";
        CapabilityBoundingSet = "";
        LockPersonality = true;
        NoNewPrivileges = true;
        PrivateDevices = true;
        PrivateTmp = true;
        ProtectClock = true;
        ProtectControlGroups = true;
        ProtectHome = true;
        ProtectHostname = true;
        ProtectKernelLogs = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        ProtectProc = "invisible";
        ProtectSystem = "strict";
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        SystemCallArchitectures = "native";
        UMask = "0077";
        ReadWritePaths = [ "/var/lib/alloy" ];
      };
    };
  };
}
