{ lib, stdenv, fetchurl, patchelf, makeWrapper, fontconfig, zlib, cacert }:
let
  runtimeLibs = [ stdenv.cc.cc.lib fontconfig zlib ];
in
stdenv.mkDerivation rec {
  pname = "moli";
  version = "1.1.22";
  src = fetchurl {
    url = "https://github.com/lexmount/moli/releases/download/v${version}/moli-x86_64-unknown-linux-gnu.tar.gz";
    hash = "sha256-fhD5pbYjcdpWIDshrAFyu+PDYm3hifw3QrkbLiuM3Tk=";
  };
  nativeBuildInputs = [ patchelf makeWrapper ];
  buildInputs = runtimeLibs;
  dontBuild = true;
  dontStrip = true;
  dontPatchELF = true;
  installPhase = ''
    runHook preInstall
    install -Dm755 moli "$out/bin/moli"
    patchelf --set-interpreter "$(cat "$NIX_CC/nix-support/dynamic-linker")" "$out/bin/moli"
    wrapProgram "$out/bin/moli" \
      --prefix LD_LIBRARY_PATH : ${lib.makeLibraryPath runtimeLibs} \
      --set-default SSL_CERT_FILE ${cacert}/etc/ssl/certs/ca-bundle.crt
    runHook postInstall
  '';
  doInstallCheck = true;
  installCheckPhase = ''
    "$out/bin/moli" --version
    "$out/bin/moli" serve --help > /dev/null
  '';
  meta = {
    description = "Moli headless browser with CDP and on-demand rendering";
    homepage = "https://github.com/lexmount/moli";
    license = with lib.licenses; [ asl20 mit ];
    mainProgram = "moli";
    platforms = [ "x86_64-linux" ];
    sourceProvenance = [ lib.sourceTypes.binaryNativeCode ];
  };
}
