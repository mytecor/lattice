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
    ];
  };
}
