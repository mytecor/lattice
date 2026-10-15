{ config, lib, pkgs, ... }:

let
  authFile = ./secrets/antigravity-auth.age;
  provisioned = builtins.pathExists authFile;
  gateway = config.lattice.agentrun-openai;
  antigravityHome = "/var/lib/${gateway.stateDirectory}/antigravity";
in
{
  config = lib.mkMerge [
    {
      environment.systemPackages = [
        pkgs.lattice.antigravity
        pkgs.lattice.antigravity-acp
        pkgs.lattice.antigravity-acp-fsless
      ];
    }
    (lib.mkIf provisioned {
      age.secrets.antigravity-auth = {
        file = authFile;
        mode = "0400";
      };
      lattice.agentrun-openai = {
        path = [ pkgs.lattice.antigravity-acp-fsless ];
        backends.antigravity = {
          command = "antigravity-acp-fsless";
        };
        extraEnv.GEMINI_HOME = antigravityHome;
      };
    })
  ];
}
