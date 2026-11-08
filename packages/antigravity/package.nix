{ lib
, stdenv
, fetchurl
, autoPatchelfHook ? null
}:

let
  version = "1.5.3";

  sources = {
    x86_64-linux = {
      url = "https://storage.googleapis.com/antigravity-public/antigravity-cli/${version}-6604829099950080/linux-x64/cli_linux_x64.tar.gz";
      sha256 = "1b4850599cce73a17f94ffb422df3bf9531531b77dbddc40c1a435e0c2c16807";
    };
    aarch64-darwin = {
      url = "https://storage.googleapis.com/antigravity-public/antigravity-cli/${version}-6604829099950080/darwin-arm/cli_mac_arm64.tar.gz";
      sha256 = "b47034e1367fc3c9f2856f1e1ae30bf12819a7213821ba3cce2123b090f2df8a";
    };
  };

  srcInfo = sources.${stdenv.hostPlatform.system} or (throw "antigravity: unsupported platform ${stdenv.hostPlatform.system}");
in
stdenv.mkDerivation {
  pname = "antigravity";
  inherit version;

  src = fetchurl {
    inherit (srcInfo) url sha256;
  };

  sourceRoot = ".";

  nativeBuildInputs = lib.optionals stdenv.hostPlatform.isLinux (lib.optional (autoPatchelfHook != null) autoPatchelfHook);
  buildInputs = lib.optionals stdenv.hostPlatform.isLinux [ stdenv.cc.cc.lib ];

  installPhase = ''
    runHook preInstall
    mkdir -p "$out/bin"
    install -m 0755 antigravity "$out/bin/antigravity"
    ln -s antigravity "$out/bin/agy"
    runHook postInstall
  '';

  meta = {
    description = "Google Antigravity CLI (agy) — terminal AI coding agent harness";
    homepage = "https://antigravity.google";
    license = lib.licenses.unfree;
    mainProgram = "antigravity";
    platforms = builtins.attrNames sources;
  };
}
