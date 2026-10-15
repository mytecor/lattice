{ config, lib, pkgs, ... }:

let
  authFile = ./secrets/gemini-auth.age;
  provisioned = builtins.pathExists authFile;
  gateway = config.lattice.agentrun-openai;
  geminiHome = "/var/lib/${gateway.stateDirectory}/gemini";
in
{
  config = lib.mkMerge [
    {
      environment.systemPackages = [
        pkgs.lattice.gemini-cli
        pkgs.lattice.gemini-acp
        pkgs.lattice.gemini-acp-fsless
      ];
    }
    (lib.mkIf provisioned {
      age.secrets.gemini-auth = {
        file = authFile;
        mode = "0400";
      };
      lattice.agentrun-openai = {
        path = [ pkgs.lattice.gemini-acp-fsless ];
        backends.gemini = {
          command = "gemini-acp-fsless";
        };
        extraEnv.GEMINI_HOME = geminiHome;
        extraEnv.GEMINI_CLI_HOME = geminiHome;
      };
    })
  ];
}
