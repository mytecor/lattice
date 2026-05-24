{ writeShellApplication }:

writeShellApplication {
  name = "lattice-example";

  text = ''
    printf 'hello from lattice example package\n'
  '';
}
