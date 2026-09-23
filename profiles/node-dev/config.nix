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

{ lib, pkgs, ... }:

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
          "rad://z3AqC22BKQ5Gnrkw49N7PGJa91G6L/z6Mkvq7AcVgfLmaecxQEasuErFk6s7fLDj2668WLBFCE9xWV";
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
    ];
  };
}
