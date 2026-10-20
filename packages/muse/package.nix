{ lib, stdenvNoCC, fetchurl, makeWrapper, cacert }:

let
  version = "1.4.11-R6667.1";
  sources = {
    x86_64-linux = {
      artifact = "muse-x86-linux";
      sha256 = "6fda12a6a4f236528dafa707384cf6395fe859b1438ef77c7be26ed9d7976839";
    };
    aarch64-linux = {
      artifact = "muse-aarch64-linux";
      sha256 = "b1ce49b4d988a1d6b1d76994d6c7a0eae912c8ed2c1cf67ac89434c017c8232e";
    };
  };
  source = sources.${stdenvNoCC.hostPlatform.system};
in
stdenvNoCC.mkDerivation {
  pname = "muse";
  inherit version;
  src = fetchurl {
    url = "https://lookaside.facebook.com/lookaside/muse/download/?channel=muse&version=${version}&file=${source.artifact}";
    inherit (source) sha256;
  };
  nativeBuildInputs = [ makeWrapper ];
  dontUnpack = true;
  # Upstream ships a static ELF with embedded resources; preserve it verbatim.
  dontStrip = true;
  dontPatchELF = true;
  installPhase = ''
    runHook preInstall
    install -Dm755 "$src" "$out/libexec/muse"
    makeWrapper "$out/libexec/muse" "$out/bin/muse" \
      --set-default SSL_CERT_FILE "${cacert}/etc/ssl/certs/ca-bundle.crt"
    runHook postInstall
  '';
  doInstallCheck = true;
  installCheckPhase = ''
    runHook preInstallCheck
    export HOME="$TMPDIR/muse-home"
    mkdir -p "$HOME"
    "$out/bin/muse" --version | grep -F '${version}'
    "$out/bin/muse" serve --help >/dev/null
    runHook postInstallCheck
  '';
  meta = {
    description = "Meta Muse Code terminal coding agent";
    homepage = "https://dev.meta.ai/docs/muse-code";
    license = lib.licenses.unfree;
    mainProgram = "muse";
    platforms = builtins.attrNames sources;
    sourceProvenance = [ lib.sourceTypes.binaryNativeCode ];
  };
}
