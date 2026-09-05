{ ... }:

let
  branch = "main";
  latticeRepository = (import ../radicle/repositories.nix).lattice;
in
{
  config.services.comin = {
    enable = true;
    remotes = [
      {
        name = "radicle";
        # Radicle maintains this as a bare repository and exposes the
        # authoritative branch at refs/heads/main. A clean node can fall back
        # to GitHub until radicle-seed-lattice has populated the storage.
        url = latticeRepository.storagePath;
        branches.${branch}.name = branch;
      }
      {
        name = "origin";
        url = "https://github.com/mytecor/lattice.git";
        branches.${branch}.name = branch;
      }
    ];
  };
}
