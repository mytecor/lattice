let
  latticeRid = "rad:z3AqC22BKQ5Gnrkw49N7PGJa91G6L";
in
{
  lattice = {
    rid = latticeRid;
    storagePath =
      "/var/lib/radicle/storage/${builtins.replaceStrings [ "rad:" ] [ "" ] latticeRid}";
  };
}
