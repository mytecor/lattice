{ nixpkgs, pkgs, ipfsRegistryFacadeModule }:

let
  inherit (nixpkgs) lib;

  config = (lib.nixosSystem {
    modules = [
      ipfsRegistryFacadeModule
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "node-a";
        system.stateVersion = "26.05";

        lattice.ipfs-registry-facade = {
          enable = true;
          listenAddress = "127.0.0.1";
          port = 5050;
          dataDir = "/var/lib/ipfs-daemon";
          ipfsApiAddress = "/ip4/127.0.0.1/tcp/5001";
        };
      }
    ];
  }).config;

  cfg = config.lattice.ipfs-registry-facade;
  facadeUnit = config.systemd.services.ipfs-registry-facade;
in
assert cfg.enable;
assert cfg.port == 5050;
assert cfg.listenAddress == "127.0.0.1";
assert cfg.dataDir == "/var/lib/ipfs-daemon";
# 1. Kubo service is enabled and configured with persistent repo dir
assert config.services.kubo.enable;
assert config.services.kubo.dataDir == "/var/lib/ipfs-daemon";
assert config.services.kubo.settings.Addresses.API == [ "/ip4/127.0.0.1/tcp/5001" ];
assert config.services.kubo.settings.Addresses.Gateway == [ ];
assert config.systemd.services.ipfs.serviceConfig.StateDirectory == "ipfs-daemon";
assert config.systemd.services.ipfs.serviceConfig.StateDirectoryMode == "0750";
# 2. Registry facade service starts at multi-user, ordered after ipfs.service
assert lib.elem "multi-user.target" facadeUnit.wantedBy;
assert lib.elem "ipfs.service" facadeUnit.after;
assert lib.elem "ipfs.service" facadeUnit.requires;
# 3. Registry facade execs nerdctl ipfs registry serve with configured flags
assert lib.hasInfix "nerdctl" facadeUnit.serviceConfig.ExecStart;
assert lib.hasInfix "ipfs registry serve" facadeUnit.serviceConfig.ExecStart;
assert lib.hasInfix "--listen-registry 127.0.0.1:5050" facadeUnit.serviceConfig.ExecStart;
assert lib.hasInfix "--ipfs-address /ip4/127.0.0.1/tcp/5001" facadeUnit.serviceConfig.ExecStart;
# 4. Strict sandbox: no new privileges, full system protection
assert facadeUnit.serviceConfig.NoNewPrivileges == true;
assert facadeUnit.serviceConfig.ProtectSystem == "full";
assert lib.elem "AF_UNIX" facadeUnit.serviceConfig.RestrictAddressFamilies;
assert lib.elem "AF_INET" facadeUnit.serviceConfig.RestrictAddressFamilies;
assert lib.elem "AF_INET6" facadeUnit.serviceConfig.RestrictAddressFamilies;
# 5. Required tools in environment packages
assert lib.elem pkgs.nerdctl config.environment.systemPackages;
assert lib.elem pkgs.kubo config.environment.systemPackages;
pkgs.runCommand "ipfs-registry-facade-evaluation" { } ''
  echo "ipfs-registry-facade contract verified" > $out
''
