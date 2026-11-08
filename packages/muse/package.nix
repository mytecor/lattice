{ lib, stdenvNoCC, fetchurl, makeWrapper, cacert }:

let
  version = "1.4.14-R7309.1";
  sources = {
    x86_64-linux = {
      artifact = "muse-x86-linux";
      sha256 = "0809aac7cb58d18f5022b6d05e02aa1974683c0ec430e5f04d25b60ed17de2da";
    };
    aarch64-linux = {
      artifact = "muse-aarch64-linux";
      sha256 = "3dc39b8fa26f9963cc2038f2966c0932ed5a39f0f9efb39cc0266a261cbbcf76";
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
