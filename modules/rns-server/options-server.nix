{ lib, ... }:

let
  inherit (lib) mkOption types;
  nullable = type: default: description: mkOption { inherit type default description; };
  nullableOpt = type: description: nullable (types.nullOr type) null description;
in
{
  options.lattice.rns-server.server = {
    statsDbPath = nullableOpt types.str "Path to the rns-statsd SQLite database.";
    rnsdBin = nullableOpt types.str "Advanced override for the rnsd executable.";
    sentineldBin = nullableOpt types.str "Advanced override for the rns-sentineld executable.";
    statsdBin = nullableOpt types.str "Advanced override for the rns-statsd executable.";
    http = {
      enabled = nullableOpt types.bool "Enable the embedded HTTP control plane.";
      host = nullableOpt types.str "Embedded HTTP bind host.";
      port = nullableOpt types.port "Embedded HTTP bind port.";
      authToken = nullableOpt types.str "Embedded HTTP bearer token.";
      disableAuth = nullableOpt types.bool "Disable embedded HTTP authentication.";
    };
  };
}
