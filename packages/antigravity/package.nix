{ lib
, stdenv
, fetchurl
, autoPatchelfHook ? null
}:

let
  version = "1.3.5";

  sources = {
    x86_64-linux = {
      url = "https://storage.googleapis.com/antigravity-public/antigravity-cli/${version}-6164039479459840/linux-x64/cli_linux_x64.tar.gz";
      sha256 = "007a5a44dbaf1caea464c3d9848c63b2f69945b01a8bf8852c56c477a64374cf";
    };
    aarch64-darwin = {
      url = "https://storage.googleapis.com/antigravity-public/antigravity-cli/${version}-6164039479459840/darwin-arm/cli_mac_arm64.tar.gz";
      sha256 = "60b46713d0e7217b796560ffc1c3c569f4e0b3357076ee6b60d464832d6d4105";
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
