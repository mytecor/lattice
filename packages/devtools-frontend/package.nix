{ lib, stdenvNoCC, fetchurl, unzip }:
stdenvNoCC.mkDerivation {
  pname = "chrome-devtools-frontend";
  version = "154.0.8022.0-snapshot-1684572";
  src = fetchurl {
    url = "https://storage.googleapis.com/chromium-browser-snapshots/Linux_x64/1684572/devtools-frontend.zip";
    hash = "sha256-TQHbTO7Z6Y5DTK/jMmT7R9DuKsfXjqjO6Da/8KXeRfk=";
  };
  nativeBuildInputs = [ unzip ];
  dontBuild = true;
  installPhase = ''
    runHook preInstall
    mkdir -p "$out"
    cp -r gen/third_party/devtools-frontend/src/front_end/. "$out/"
    # Chromium packaging also includes source maps, hashes and duplicate
    # compressed resources. They are not needed by the hosted frontend.
    find "$out" -type f \( -name '*.map' -o -name '*.hash' -o -name '*.compressed' \) -delete
    install -Dm644 ${./LICENSE} "$out/LICENSE"
    test -s "$out/inspector.html"
    test -s "$out/entrypoints/inspector/inspector.js"
    runHook postInstall
  '';
  meta = {
    description = "Official Chromium DevTools frontend for hosted debugging";
    homepage = "https://chromium.googlesource.com/devtools/devtools-frontend/";
    license = lib.licenses.bsd3;
    platforms = lib.platforms.all;
  };
}
