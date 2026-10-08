{ config, lib, pkgs, ... }:

let
  authFile = ./secrets/codex-auth.age;
  provisioned = builtins.pathExists authFile;
  gateway = config.lattice.agentrun-openai;
  codexHome = "/var/lib/${gateway.stateDirectory}/codex";
  seedAuth = pkgs.writeShellApplication {
    name = "seed-codex-auth";
    runtimeInputs = [ pkgs.coreutils pkgs.jq ];
    text = builtins.readFile ../../scripts/seed-codex-auth.sh;
  };
in
{
  # No unauthenticated backend in the catalog before credentials are provisioned.
  config = lib.mkIf provisioned {
    age.secrets.codex-auth = {
      file = authFile;
      mode = "0400";
    };
    lattice.agentrun-openai = {
      path = [ pkgs.lattice.codex-acp-fsless ];
      backends.codex = {
        command = "codex-acp-fsless";
      };
      extraEnv.CODEX_HOME = codexHome;
    };
    systemd.services.agentrun-openai = {
      serviceConfig.LoadCredential = [ "codex-auth:${config.age.secrets.codex-auth.path}" ];
      # Runs before any agents start. The writable copy lets Codex rotate tokens;
      # importing the same seed again must not revert that rotation.
      preStart = ''
        ${lib.getExe seedAuth} "$CREDENTIALS_DIRECTORY/codex-auth" ${lib.escapeShellArg codexHome}
      '';
      restartTriggers = [ authFile ];
    };
  };
}
