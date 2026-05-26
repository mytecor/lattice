# rnsh module

NixOS module for running an `rnsh` listener service from the shared `rns-rs` package.

```nix
{
  lattice.rnsh = {
    enable = true;
    configDir = "/var/lib/rns";
    noAuth = true;
    command = [ "/bin/sh" ];
  };
}
```

By default, the service runs `rnsh -l --config /var/lib/rns` as the `rns` user.
