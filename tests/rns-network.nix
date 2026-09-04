{ nixpkgs, pkgs, rnsModule, networkProfile }:

let
  inherit (nixpkgs) lib;
  mkConfig = extra: (lib.nixosSystem {
    modules = [
      rnsModule
      networkProfile
      {
        nixpkgs.pkgs = nixpkgs.legacyPackages.x86_64-linux;
        system.stateVersion = "26.05";
        networking.hostName = lib.mkDefault "client-a";
        lattice.rns-server.package = nixpkgs.legacyPackages.x86_64-linux.hello;
      }
      extra
    ];
  }).config;
  client = mkConfig { };
  secondClient = mkConfig { networking.hostName = "client-b"; };
  custom = mkConfig {
    lattice.rns-network.uplinks = {
      Primary = { host = "next.example.net"; port = 14243; };
      Disabled = { host = "disabled.example.net"; enable = false; };
      IPv6 = { host = "[::1]"; };
    };
  };
  failures = config: map (item: item.message) (lib.filter
    (item: !item.assertion && lib.hasPrefix "lattice.rns-" item.message) config.assertions);
  bad = peers: mkConfig { lattice.rns-network.uplinks = peers; };
  evaluates = peer: (builtins.tryEval (builtins.deepSeq
    (bad { Test = peer; }).lattice.rns-network.uplinks true)).success;
  clientFile = pkgs.writeText "rns-network-client" client.lattice.rns-server.configFile.text;
  customFile = pkgs.writeText "rns-network-custom" custom.lattice.rns-server.configFile.text;
in
assert failures client == [ ];
assert failures custom == [ ];
assert client.lattice.rns-server.enable;
assert !client.lattice.rns-server.server.http.enabled;
assert client.networking.firewall.allowedTCPPorts == [ ];
assert custom.networking.firewall.allowedTCPPorts == [ ];
assert client.lattice.rns-server.configFile.text == secondClient.lattice.rns-server.configFile.text;
assert failures (bad { }) != [ ];
assert failures (bad { Off = { host = "example.net"; enable = false; }; }) != [ ];
assert failures (bad { "bad]name" = { host = "example.net"; }; }) != [ ];
assert failures (bad { One = { host = "example.net"; }; Two = { host = "example.net"; }; }) != [ ];
assert !evaluates { host = ""; };
assert !evaluates { host = "host\nextra = yes"; };
assert !evaluates { host = "host,second"; };
assert !evaluates { host = "host"; port = 0; };
assert !evaluates { host = "host"; port = 65536; };
assert (bad { Test.host = "example.net"; }).lattice.rns-network.uplinks.Test.port == 4242;
pkgs.runCommand "rns-network-config-check" {
  nativeBuildInputs = [ (pkgs.python3.withPackages (python: [ python.configobj ])) ];
} ''
  python ${./rns-network-config.py} ${clientFile} ${customFile}
  mkdir -p "$out"
  cp ${clientFile} "$out/client.config"
  cp ${customFile} "$out/custom.config"
''
