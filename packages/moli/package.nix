{ lib, stdenv, fetchurl, autoPatchelfHook, makeWrapper, fontconfig, zlib, cacert }:
stdenv.mkDerivation rec {
  pname = "moli";
  version = "1.1.18";
  src = fetchurl {
    url = "https://github.com/lexmount/moli/releases/download/v${version}/moli-x86_64-unknown-linux-gnu.tar.gz";
    hash = "sha256-WXFfvbKwFruRNuHttZoqGVwPmEWf0Gc54X7cYtpqSLE=";
  };
  nativeBuildInputs = [ autoPatchelfHook makeWrapper ];
  buildInputs = [ stdenv.cc.cc.lib fontconfig zlib ];
  dontBuild = true;
  installPhase = ''
    runHook preInstall
    install -Dm755 moli "$out/bin/moli"
    wrapProgram "$out/bin/moli" --set-default SSL_CERT_FILE ${cacert}/etc/ssl/certs/ca-bundle.crt
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
