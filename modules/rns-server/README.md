# rns-server module

NixOS module for running `rns-server` and generating the RNS ConfigObj files.

```nix
{
  lattice.rns-server = {
    enable = true;

    interfaces = {
      "Auto Discovery" = {
        type = "AutoInterface";
        enabled = true;
        discovery_scope = "link";
        discovery_port = 29716;
        data_port = 42671;
      };

      "TCP Uplink" = {
        type = "TCPClientInterface";
        target_host = "entry.example.net";
        target_port = 4242;
      };

      "TCP Server" = {
        type = "TCPServerInterface";
        listen_ip = "0.0.0.0";
        listen_port = 4242;
        max_connections = 64;
        openFirewall = true;
      };

      "Local UDP" = {
        type = "UDPInterface";
        address = "0.0.0.0:4242";
      };
    };
  };
}
```

The module writes `config` and `rns-server.json` into `configDir`, which defaults to `/var/lib/rns`.
The generated ConfigObj derivation is also available as `lattice.rns-server.configFile`.
These files are public Nix store artifacts; do not put passwords or private keys in the options.

## TCP interfaces

An enabled `TCPClientInterface` requires explicit `target_host` and `target_port`. An enabled
`TCPServerInterface` requires `listen_ip` and `listen_port`. Ports must be in the range 1–65535;
hosts cannot be empty or contain whitespace, `#` or `=`. With the pinned `rns-rs` version, use
brackets around IPv6 literals (`[::1]`, `[::]`) because the driver joins host and port with `:`.
`entry.example.net` above is an example address, not an existing Lattice entry point.

`max_connections` is an optional positive server connection limit. `openFirewall` defaults to
`false`; enabling it opens only the configured TCP server's `listen_port`. Disabled interfaces
and a disabled module do not open ports. TCP clients cannot set `openFirewall`. This option is
consumed by NixOS and is not written to the Reticulum config. Router/NAT forwarding is configured
separately; opening the local firewall does not provide a public address.

Use typed connection fields, not `extraConfig` overrides for `type`, `enabled`, `target_host`,
`target_port`, `listen_ip`, `listen_port`, `max_connections` or `openFirewall`. TCP does not use the
UDP `address` or generic `port` aliases. Disabled TCP entries may omit their connection parameters,
so a profile can contain an uplink template until its entry point is chosen.

The [Lattice profile](../../profiles/rns-server/README.md) adds defaults for a TCP listener and an
optional uplink alongside AutoInterface. Transport routing between peers remains an explicit
`lattice.rns-server.reticulum.enable_transport = true` choice for a gateway role.

`checks.x86_64-linux.rns-tcp` checks defaults, overrides, rejected configurations, firewall rules
and the generated files using an independent ConfigObj parser:

```sh
nix build .#checks.x86_64-linux.rns-tcp
```
