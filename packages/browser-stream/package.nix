{ lib, stdenvNoCC, fetchurl, nodejs, makeWrapper }:
let
  phantom = fetchurl {
    url = "https://registry.npmjs.org/@full-self-browsing/phantom-stream/-/phantom-stream-0.2.1.tgz";
    hash = "sha512-3aG66I7IHMml8H2kORlghwobVyIj9/YZaVxGkGMaRYPOEO19RabFZU/R4K1RG3Epf0bSfjbFDDxyGeXmgkXd5A==";
  };
  ws = fetchurl {
    url = "https://registry.npmjs.org/ws/-/ws-8.21.0.tgz";
    hash = "sha512-Vsp28b7DRcimFQvrqu2Wek3z1iYxDCWqHYB8Qsnk/S4RfaCQzPGPyBNuVjJV3cd6UiKtUtp6sNM77gWvzcCH+g==";
  };
in stdenvNoCC.mkDerivation {
  pname = "lattice-browser-stream";
  version = "0.1.0";
  src = lib.cleanSource ./.;
  nativeBuildInputs = [ nodejs makeWrapper ];
  # These two pure-JS packages have no required transitive dependencies.
  # Extract hash-pinned npm artifacts; never run a networked npm install in builds.
  configurePhase = ''
    mkdir -p node_modules/@full-self-browsing/phantom-stream node_modules/ws
    tar xf ${phantom} -C node_modules/@full-self-browsing/phantom-stream --strip-components=1
    tar xf ${ws} -C node_modules/ws --strip-components=1
  '';
  dontBuild = true;
  doCheck = true;
  checkPhase = ''
    node --test *.test.js
  '';
  installPhase = ''
    mkdir -p "$out/libexec/browser-stream" "$out/bin"
    cp server.js streams.js cdp.js control.js package.json "$out/libexec/browser-stream/"
    cp -r public node_modules "$out/libexec/browser-stream/"
    makeWrapper ${lib.getExe nodejs} "$out/bin/lattice-browser-stream" \
      --add-flags "$out/libexec/browser-stream/server.js"
  '';
  meta = {
    description = "PhantomStream DOM viewer for the Lattice Foxbridge browser";
    license = lib.licenses.mit;
    mainProgram = "lattice-browser-stream";
    platforms = lib.platforms.linux;
  };
}
