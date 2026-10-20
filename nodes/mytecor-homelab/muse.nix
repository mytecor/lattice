{ config, pkgs, ... }:

let
  museHome = "/var/lib/${config.lattice.agentrun-openai.stateDirectory}/muse";
  login = pkgs.writeShellApplication {
    name = "muse-agentrun";
    text = ''
      export MUSE_HOME=${museHome}
      exec ${pkgs.lattice.muse-acp-fsless.cli}/bin/muse-agentrun "$@"
    '';
  };
in
{
  environment.systemPackages = [ pkgs.lattice.muse login ];
  lattice.agentrun-openai = {
    path = [ pkgs.lattice.muse-acp-fsless ];
    backends.muse.command = "muse-acp-fsless";
    extraEnv.MUSE_HOME = museHome;
  };
}
