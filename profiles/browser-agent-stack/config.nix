{
  lib,
  ...
}:

let
  latticePorts = import ../networking/ports.nix;
in
{
  # F18: browser agent stack — long-running browser runtime (Foxbridge +
  # Camoufox) and the Jev agent that consumes it via BU_CDP_URL. Both
  # loopback-only. Secrets (Jev API keys) are wired at the node as agenix
  # secrets via typesafeApiKeyFile / textModelApiKeyFile; the services come up
  # in inspector mode without them (see modules/services/*-README).
  lattice.foxbridge-camoufox = {
    enable = lib.mkDefault true;
    listenAddress = lib.mkDefault "127.0.0.1";
    port = lib.mkDefault latticePorts.foxbridge-cdp;
    camoufox = {
      headless = lib.mkDefault true;
      humanize = lib.mkDefault true;
    };
  };

  lattice.jev-ultrafast = {
    enable = lib.mkDefault true;
    cdpUrl = lib.mkDefault "http://127.0.0.1:${toString latticePorts.foxbridge-cdp}";
    inspectorPort = lib.mkDefault latticePorts.jev-inspector;
  };
}
