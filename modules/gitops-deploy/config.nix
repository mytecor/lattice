{ lib, ... }:

{
  services.comin = {
    enable = lib.mkDefault true;
    remotes = lib.mkDefault [
      {
        name = "origin";
        url = "https://github.com/mytecor/lattice.git";
        branches.main.name = "main";
      }
    ];
  };
}
