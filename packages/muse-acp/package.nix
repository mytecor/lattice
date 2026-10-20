{ lib, stdenv, fetchurl, autoPatchelfHook, makeWrapper, muse }:

let
  version = "0.15.0";
  sources = {
    x86_64-linux = {
      target = "x86_64-unknown-linux-gnu";
      sha256 = "16e193de0d09e3a00395312665176e98685ce5a68f1652fd2f049db6850b0d47";
    };
    aarch64-linux = {
      target = "aarch64-unknown-linux-gnu";
      sha256 = "790c224ab3ea65c21449a3432b71bbe79a695e0ade839be9993eecd0f007dacf";
    };
  };
  source = sources.${stdenv.hostPlatform.system};
in
stdenv.mkDerivation {
  pname = "muse-acp";
  inherit version;
  src = fetchurl {
    url = "https://github.com/BrokkAi/muse-acp/releases/download/v${version}/muse-acp-v${version}-${source.target}.tar.gz";
    inherit (source) sha256;
  };
  sourceRoot = "muse-acp-v${version}-${source.target}";
  nativeBuildInputs = [ autoPatchelfHook makeWrapper ];
  buildInputs = [ stdenv.cc.cc.lib ];
  installPhase = ''
    runHook preInstall
    install -Dm755 muse-acp "$out/libexec/muse-acp"
    makeWrapper "$out/libexec/muse-acp" "$out/bin/muse-acp" \
      --set-default MUSE_CLI ${lib.getExe muse}
    runHook postInstall
  '';
  doInstallCheck = true;
  installCheckPhase = ''
    runHook preInstallCheck
    export HOME="$TMPDIR/muse-acp-home"
    mkdir -p "$HOME"
    "$out/bin/muse-acp" --selftest
    runHook postInstallCheck
  '';
  meta = {
    description = "ACP stdio adapter for the Muse Session Protocol";
    homepage = "https://github.com/BrokkAi/muse-acp";
    license = lib.licenses.asl20;
    mainProgram = "muse-acp";
    platforms = builtins.attrNames sources;
    sourceProvenance = [ lib.sourceTypes.binaryNativeCode ];
  };
}
