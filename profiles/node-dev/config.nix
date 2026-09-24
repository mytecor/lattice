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
  # f15-02/acceptance: the node's own Radicle peer profile. The `rad://` push
  # URL must carry the node's peer DID (a repository-scoped URL without a DID
  # makes git-remote-rad fail with "no public key given as remote namespace";
  # the plain git checkout has no in-repo rad context to derive it from).
  radiclePeerHome = "/persist/var/lib/radicle-peer";
  latticeRid = latticeRepository.rid;
  # `rid` carries the `rad:` scheme prefix; the git remote URL wants the bare
  # NID (a rad:// URL embeds the scheme itself).
  latticeNid = builtins.replaceStrings [ "rad:" ] [ "" ] latticeRid;
  # Node peer DID (public, generated on the node 2026-09-23; committed in the
  # RID identity revision d888fa4 as delegate, threshold 1-of-2).
  radiclePeerDid = "z6MkqUjzpiYfDAcjnj2379bYfEk4DdLtWQkyfk7nECn6HyZx";
  radiclePushUrl = "rad://${latticeNid}/${radiclePeerDid}";
in
{
  config = {
    # f15-02: rad-peer on the system PATH too, so the operator/ad-hoc shell can
    # run `rad-peer auth status` / bootstrap the peer identity without digging a
    # store path. Sessions additionally get it through
    # lattice.pi-acp-daemon.path (node config).
    environment.systemPackages = [ pkgs.lattice.rad-peer ];

    # f15-02/acceptance: the node's Radicle *peer* profile needs its own node
    # process so `git push rad://` from a session has a running peer to sign
    # with and sync through. This is a second radicle-node on the host, bound to
    # RAD_HOME=/persist/var/lib/radicle-peer (strictly separate from the seed
    # profile /var/lib/radicle), with no listening socket (listen: []) so it
    # never collides with services.radicle (seed node on [::]:8776).
    systemd.services.radicle-peer-node = {
      description = "Radicle peer node for the node dev-loop";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      path = [ pkgs.radicle-node ];
      environment = {
        RAD_HOME = radiclePeerHome;
        RAD_LOG = "info";
      };
      serviceConfig = {
        Type = "simple";
        ExecStart = "${pkgs.radicle-node}/bin/radicle-node --force";
        Restart = "on-failure";
        RestartSec = "30s";
        User = "root";
        Group = "root";
      };
    };

    # f15-02/acceptance: bootstrap the Lattice RID into the peer profile so the
    # radicle remote helper can resolve rad:// z3AqC... locally (its storage is
    # empty after `rad-peer auth`; a bare push fails with "storage/… not found").
    systemd.services.radicle-peer-seed-lattice = {
      description = "Bootstrap the Lattice repository into the node's Radicle peer profile";
      after = [ "radicle-peer-node.service" ];
      requires = [ "radicle-peer-node.service" ];
      wantedBy = [ "multi-user.target" ];
      path = [ pkgs.radicle-node ];
      environment.RAD_HOME = radiclePeerHome;
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        Restart = "on-failure";
        RestartSec = "1min";
        TimeoutStartSec = "6min";
        User = "root";
        Group = "root";
        ExecStart = lib.concatStringsSep " " [
          "${pkgs.radicle-node}/bin/rad"
          "seed --scope followed --timeout 5min"
          latticeRid
        ];
      };
    };

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
        LATTICE_WORKSPACE_RADICLE_PUSH_URL = radiclePushUrl;
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
