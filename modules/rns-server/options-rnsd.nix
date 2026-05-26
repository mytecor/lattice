{ lib, ... }:

let
  inherit (lib) mkOption types;
  nullable = type: default: description: mkOption { inherit type default description; };
  nullableOpt = type: description: nullable (types.nullOr type) null description;
  number = types.oneOf [ types.int types.float ];
  scalar = types.oneOf [ types.bool types.int types.float types.str types.path ];
in
{
  options.lattice.rns-server = {
    reticulum = {
      enable_transport = nullable types.bool false "Enable transport-node routing.";
      share_instance = nullable types.bool true "Share this Reticulum instance with local clients.";
      instance_name = nullable types.str "default" "Shared instance name.";
      shared_instance_port = nullable types.port 37428 "Shared instance TCP port.";
      instance_control_port = nullable types.port 37429 "Instance control TCP port.";
      panic_on_interface_error = nullable types.bool false "Panic when an interface fails to start.";
      use_implicit_proof = nullable types.bool true "Use implicit link proof handling.";
      network_identity = nullableOpt types.str "Path to a network identity file.";
      respond_to_probes = nullable types.bool false "Respond to direct-link probes.";
      enable_remote_management = nullable types.bool false "Enable remote management.";
      remote_management_allowed = mkOption { type = types.listOf types.str; default = [ ]; description = "Allowed remote management identity hashes."; };
      publish_blackhole = nullable types.bool false "Publish blackhole path information.";
      probe_port = nullableOpt types.port "Direct-link probe listen port.";
      probe_addr = nullableOpt types.str "Direct-link probe facilitator address.";
      probe_protocol = nullableOpt (types.enum [ "rnsp" "stun" ]) "Direct-link probe protocol.";
      device = nullableOpt types.str "Network device for outbound sockets.";
      discover_interfaces = nullable types.bool false "Enable interface discovery.";
      required_discovery_value = nullableOpt types.int "Minimum discovery stamp value.";
      prefer_shorter_path = nullable types.bool false "Prefer shorter paths for duplicate announces.";
      max_paths_per_destination = nullable types.int 1 "Maximum alternative paths per destination.";
      packet_hashlist_max_entries = nullableOpt types.int "Maximum duplicate-suppression packet hashes.";
      max_discovery_pr_tags = nullableOpt types.int "Maximum discovery path-request tags.";
      max_path_destinations = nullableOpt types.int "Maximum live path destinations.";
      max_tunnel_destinations_total = nullableOpt types.int "Maximum tunnel-known destinations.";
      known_destinations_ttl = nullableOpt types.int "Known destination TTL in seconds.";
      known_destinations_max_entries = nullableOpt types.int "Maximum known destinations.";
      ratchet_expiry = nullableOpt types.int "Ratchet expiry in seconds.";
      announce_table_ttl = nullableOpt types.int "Announce table TTL in seconds.";
      announce_table_max_bytes = nullableOpt types.int "Maximum announce table bytes.";
      announce_signature_cache_enabled = nullableOpt types.bool "Enable announce signature cache.";
      announce_signature_cache_max_entries = nullableOpt types.int "Maximum announce signature cache entries.";
      announce_signature_cache_ttl = nullableOpt types.int "Announce signature cache TTL in seconds.";
      announce_queue_max_entries = nullableOpt types.int "Maximum async announce verification queue entries.";
      announce_queue_max_interfaces = nullableOpt types.int "Maximum interface-scoped announce queues.";
      announce_queue_max_bytes = nullableOpt types.int "Maximum async announce verification queue bytes.";
      announce_queue_ttl = nullableOpt types.int "Async announce verification queue TTL in seconds.";
      announce_queue_overflow_policy = nullableOpt (types.enum [ "drop_newest" "drop_oldest" "drop_worst" ]) "Async announce queue overflow policy.";
      default_ar_target = nullableOpt number "Default announce-rate target in seconds; 0 disables.";
      default_ar_penalty = nullableOpt number "Default announce-rate penalty in seconds.";
      default_ar_grace = nullableOpt types.int "Default announce-rate grace count.";
      ic_max_held_announces = nullableOpt types.int "Default ingress-control held announce limit.";
      ic_burst_hold = nullableOpt number "Default ingress-control burst hold time.";
      ic_burst_freq_new = nullableOpt number "Default new-interface announce burst threshold.";
      ic_burst_freq = nullableOpt number "Default mature-interface announce burst threshold.";
      ic_pr_burst_freq_new = nullableOpt number "Default new-interface path request burst threshold.";
      ic_pr_burst_freq = nullableOpt number "Default mature-interface path request burst threshold.";
      ic_new_time = nullableOpt number "Default ingress-control new-interface age window.";
      ic_burst_penalty = nullableOpt number "Default ingress-control burst penalty.";
      ic_held_release_interval = nullableOpt number "Default interval between released held announces.";
      egress_control = nullableOpt types.bool "Default egress path-request limiting state.";
      ec_pr_freq = nullableOpt number "Default egress path-request frequency threshold.";
      driver_event_queue_capacity = nullableOpt types.int "Driver event queue capacity.";
      interface_writer_queue_capacity = nullableOpt types.int "Interface writer queue capacity.";
      backbone_peer_pool_max_connected = nullableOpt types.int "Maximum active Backbone peer-pool connections.";
      backbone_peer_pool_failure_threshold = nullableOpt types.int "Backbone peer-pool failure threshold.";
      backbone_peer_pool_failure_window = nullableOpt types.int "Backbone peer-pool failure window in seconds.";
      backbone_peer_pool_cooldown = nullableOpt types.int "Backbone peer-pool cooldown in seconds.";
    };

    logging.loglevel = mkOption {
      type = types.int;
      default = 4;
      description = "RNS log level written to the [logging] section.";
    };

    interfaces = mkOption {
      type = types.attrsOf (types.submodule {
        options = {
          type = mkOption {
            type = types.enum [ "AutoInterface" "BackboneInterface" "TCPClientInterface" "TCPServerInterface" "UDPInterface" "I2PInterface" "SerialInterface" "KISSInterface" "RNodeInterface" "PipeInterface" ];
            description = "RNS interface type.";
          };
          enabled = nullable types.bool true "Enable this interface.";
          mode = nullableOpt (types.enum [ "full" "access_point" "ap" "pointtopoint" "ptp" "roaming" "boundary" "gateway" "gw" ]) "Interface mode.";
          interface_mode = nullableOpt types.str "Raw interface_mode override.";

          group_id = nullableOpt types.str "AutoInterface multicast group id.";
          discovery_scope = nullableOpt (types.enum [ "link" "admin" "site" "organisation" "organization" "global" ]) "AutoInterface discovery scope.";
          discovery_port = nullableOpt types.port "AutoInterface discovery port.";
          data_port = nullableOpt types.port "AutoInterface data port.";
          multicast_address_type = nullableOpt (types.enum [ "temporary" "permanent" ]) "AutoInterface multicast address type.";
          configured_bitrate = nullableOpt types.int "Configured interface bitrate.";
          bitrate = nullableOpt types.int "Configured interface bitrate alias.";
          devices = mkOption { type = types.listOf types.str; default = [ ]; description = "Allowed AutoInterface devices."; };
          allowed_interfaces = mkOption { type = types.listOf types.str; default = [ ]; description = "Allowed AutoInterface interfaces."; };
          ignored_devices = mkOption { type = types.listOf types.str; default = [ ]; description = "Ignored AutoInterface devices."; };
          ignored_interfaces = mkOption { type = types.listOf types.str; default = [ ]; description = "Ignored AutoInterface interfaces."; };

          target_host = nullableOpt types.str "TCP/Backbone target host.";
          target_port = nullableOpt types.port "TCP/Backbone target port.";
          remote = nullableOpt types.str "Backbone remote host alias.";
          transport_identity = nullableOpt types.str "Backbone transport identity hash.";
          priority = nullableOpt types.int "Backbone peer priority.";

          address = nullableOpt types.str "UDP listen address in host:port form.";
          forward_address = nullableOpt types.str "UDP forward address in host:port form.";
          listen_ip = nullableOpt types.str "Listen address.";
          listen_port = nullableOpt types.port "Listen port.";
          forward_ip = nullableOpt types.str "UDP forward host.";
          forward_port = nullableOpt types.port "UDP forward port.";
          port = nullableOpt (types.oneOf [ types.port types.str ]) "Port number or serial device path, depending on interface type.";
          max_connections = nullableOpt types.int "Maximum accepted connections.";
          idle_timeout = nullableOpt number "Backbone idle timeout in seconds.";
          write_stall_timeout = nullableOpt number "Backbone write-stall timeout in seconds.";
          max_penalty_duration = nullableOpt number "Backbone abuse maximum penalty duration in seconds.";

          sam_host = nullableOpt types.str "I2P SAM host.";
          sam_port = nullableOpt types.port "I2P SAM port.";
          connectable = nullableOpt types.bool "Whether this I2P interface is connectable.";
          peers = mkOption { type = types.listOf types.str; default = [ ]; description = "I2P peers."; };
          storage_dir = nullableOpt types.str "I2P storage directory.";

          speed = nullableOpt types.int "Serial/KISS/RNode baud rate.";
          databits = nullableOpt types.int "Serial/KISS data bits.";
          parity = nullableOpt (types.enum [ "N" "E" "O" "none" "even" "odd" ]) "Serial/KISS parity.";
          stopbits = nullableOpt types.int "Serial/KISS stop bits.";
          preamble = nullableOpt types.int "KISS preamble.";
          txtail = nullableOpt types.int "KISS TX tail.";
          persistence = nullableOpt types.int "KISS persistence.";
          slottime = nullableOpt types.int "KISS slot time.";
          flow_control = nullableOpt types.bool "KISS/RNode flow control.";
          beacon_interval = nullableOpt types.int "KISS beacon interval.";
          beacon_data = nullableOpt types.str "KISS beacon data.";
          frequency = nullableOpt types.int "RNode frequency.";
          bandwidth = nullableOpt types.int "RNode bandwidth.";
          txpower = nullableOpt types.int "RNode transmit power.";
          spreadingfactor = nullableOpt types.int "RNode spreading factor.";
          spreading_factor = nullableOpt types.int "RNode spreading factor alias.";
          codingrate = nullableOpt types.int "RNode coding rate.";
          coding_rate = nullableOpt types.int "RNode coding rate alias.";
          st_alock = nullableOpt number "RNode short-term airtime lock.";
          lt_alock = nullableOpt number "RNode long-term airtime lock.";
          id_interval = nullableOpt types.int "RNode/KISS id interval.";
          id_callsign = nullableOpt types.str "RNode/KISS id callsign.";
          fd = nullableOpt types.int "Pre-opened RNode file descriptor.";

          command = nullableOpt types.str "PipeInterface command.";
          respawn_delay = nullableOpt types.int "PipeInterface respawn delay in milliseconds.";

          networkname = nullableOpt types.str "IFAC network name.";
          network_name = nullableOpt types.str "IFAC network name alias.";
          passphrase = nullableOpt types.str "IFAC passphrase.";
          pass_phrase = nullableOpt types.str "IFAC passphrase alias.";
          ifac_size = nullableOpt types.int "IFAC size in bits.";

          ingress_control = nullableOpt types.bool "Enable ingress control.";
          ic_max_held_announces = nullableOpt types.int "Ingress-control held announce limit.";
          ic_burst_hold = nullableOpt number "Ingress-control burst hold time.";
          ic_burst_freq_new = nullableOpt number "New-interface announce burst threshold.";
          ic_burst_freq = nullableOpt number "Mature-interface announce burst threshold.";
          ic_pr_burst_freq_new = nullableOpt number "New-interface path request burst threshold.";
          ic_pr_burst_freq = nullableOpt number "Mature-interface path request burst threshold.";
          ic_new_time = nullableOpt number "Ingress-control new-interface age window.";
          ic_burst_penalty = nullableOpt number "Ingress-control burst penalty.";
          ic_held_release_interval = nullableOpt number "Interval between released held announces.";
          egress_control = nullableOpt types.bool "Enable egress path-request limiting.";
          ec_pr_freq = nullableOpt number "Egress path-request frequency threshold.";

          discoverable = nullableOpt types.bool "Advertise this interface through discovery.";
          discovery_name = nullableOpt types.str "Discovery advertisement name.";
          announce_interval = nullableOpt types.int "Discovery announce interval in seconds.";
          discovery_stamp_value = nullableOpt types.int "Discovery stamp value.";
          reachable_on = nullableOpt types.str "Discovery reachable host/address.";
          latitude = nullableOpt number "Discovery latitude.";
          lat = nullableOpt number "Discovery latitude alias.";
          longitude = nullableOpt number "Discovery longitude.";
          lon = nullableOpt number "Discovery longitude alias.";
          height = nullableOpt number "Discovery height.";

          extraConfig = mkOption {
            type = types.attrsOf scalar;
            default = { };
            description = "Additional raw ConfigObj key-value pairs for this interface.";
          };
        };
      });
      default = { };
      example = lib.literalExpression ''
        {
          "Auto Discovery" = {
            type = "AutoInterface";
            enabled = true;
            discovery_scope = "link";
            discovery_port = 29716;
            data_port = 42671;
          };
          "Quad4 TCP" = {
            type = "TCPClientInterface";
            target_host = "rns.quad4.io";
            target_port = 4242;
          };
          "Local UDP" = {
            type = "UDPInterface";
            address = "0.0.0.0:4242";
          };
        }
      '';
      description = "RNS interfaces rendered into the [interfaces] ConfigObj section.";
    };
  };
}
