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

      "Quad4 TCP" = {
        type = "TCPClientInterface";
        target_host = "rns.quad4.io";
        target_port = 4242;
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
