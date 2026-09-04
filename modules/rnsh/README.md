# rnsh module

NixOS module for the pinned `rns-rs` remote shell listener. Example (replace the public hash):

```nix
{
  lattice.rnsh = {
    enable = true;
    homeDir = "/var/lib/rnsh";
    configDir = "/var/lib/rns";
    identity = "/var/lib/rnsh/identity";
    allowed = [ "0123456789abcdef0123456789abcdef" ];
    announcePeriod = 3600;
    command = [ "/run/current-system/sw/bin/bash" ];
  };
}
```

The service passes `--config homeDir --rnsconfig configDir`: current rnsh separates application
state from the shared Reticulum daemon config. `UMask=0077` protects newly generated keys.
The module defaults to user `rns`; the network's `profiles/rnsh/config.nix` uses a separate
`rnsh` user with read access to the `rns` group and requires an explicit nonempty allowlist.
Persist the application directory and transport state on nodes with ephemeral root.

`noAuth` defaults to false. An empty module allowlist denies initiators; `allowed_identities`
in the application directory is also read by upstream, so audit it during access revocation.
Changing CLI `allowed` alone does not remove grants from that file. Restart the listener when
revoking identities to terminate existing sessions. `--service` selects a default identity name;
with an explicit `identity`, the file determines the listener destination.
