{ lib, stdenv, fetchurl, autoPatchelfHook, makeWrapper, muse }:

let
  version = "0.21.0";
  sources = {
    x86_64-linux = {
      target = "x86_64-unknown-linux-gnu";
      sha256 = "cd7f448f0e1803b7f7e39d8646f7c0a84cc3649775f529cc5c49878b0ec47d57";
    };
    aarch64-linux = {
      target = "aarch64-unknown-linux-gnu";
      sha256 = "f1ebfff4981f2e4bc7f3196a81e862e687039d347b6cd1298f091e7a7bd43511";
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
