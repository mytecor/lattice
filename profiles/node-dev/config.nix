# f15-01: persistent Lattice working checkout on the node (node dev-loop).
#
# One-shot service that bootstraps and keeps current a real working copy of
# Lattice at LATTICE_WORKSPACE_DIR. ACP sessions (pi-acp-daemon defaultCwd,
# f15-01) open in this checkout, so an agent can edit, commit and push `main`
# directly from the node. The workspace is strictly separate from the comin
# source (/var/lib/comin) and the Radicle seed storage
# (/var/lib/radicle/storage); the init script never touches either.
#
# The profile is thin on purpose: pushes (Radicle peer identity, GitHub deploy
# key) are f15-02, so this phase only builds the checkout and the `publish`
# remote skeleton.

{ config, lib, pkgs, ... }:

let
  latticeRepository = (import ../radicle/repositories.nix).lattice;
  workspaceDir = "/var/lib/lattice-workspace";
in
{
  config = {
    systemd.services.lattice-workspace-init = {
      description = "Bootstrap and refresh the Lattice working checkout";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" "lattice-comin-source-sync.service" ];
      wants = [ "network-online.target" ];

      environment = {
        LATTICE_WORKSPACE_DIR = workspaceDir;
        # Radicle seed storage (radicle profile) owns the objects; the checkout
        # layout below (/var/lib/lattice-workspace/lattice) already contains the
        # working copy.
        LATTICE_WORKSPACE_RADICLE_REMOTE =
          "${latticeRepository.storagePath}";
        LATTICE_WORKSPACE_ORIGIN_REMOTE = "https://github.com/mytecor/lattice.git";
        LATTICE_WORKSPACE_BRANCH = "main";
        LATTICE_WORKSPACE_RADICLE_PUSH_URL =
          "rad://z3AqC22BKQ5Gnrkw49N7PGJa91G6L";
        # f15-02: GitHub deploy key for the workspace push. Registered only when
        # the operator created the .age file (node config guards the secret with
        # optionalAttrs), so the workspace push falls back to the anonymous https
        # URL until then.
        LATTICE_WORKSPACE_GITHUB_KEY_FILE =
          if builtins.hasAttr "github-lattice-deploy-key" config.age.secrets
          then config.age.secrets.github-lattice-deploy-key.path
          else "";
      };

      serviceConfig = {
        Type = "oneshot";
        ExecStart = lib.getExe pkgs.lattice.workspace-init;
        TimeoutStartSec = "6min";
        UMask = "0077";
        User = "root";
        Group = "root";
      };
    };

    # The workspace must survive node reboots (impermanence).
    environment.persistence."/persist".directories = [
      {
        directory = workspaceDir;
        user = "root";
        group = "root";
        mode = "0700";
      }
      # f15-02: root's ssh config with the github-lattice deploy-key alias (written
      # by lattice-workspace-init) must survive reboot; /root itself is ephemeral.
      {
        directory = "/root/.ssh";
        user = "root";
        group = "root";
        mode = "0700";
      }
    ];
  };
}
