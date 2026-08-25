{ ... }:

let
  branch = "main";
in
{
  config.services.comin = {
    enable = true;
    remotes = [
      {
        name = "origin";
        url = "https://github.com/mytecor/lattice.git";
        branches.${branch}.name = branch;
      }
      {
        name = "radicle";
        url = "/var/lib/radicle/storage/z3AqC22BKQ5Gnrkw49N7PGJa91G6L";
        branches.${branch}.name = branch;
      }
    ];
  };
}
