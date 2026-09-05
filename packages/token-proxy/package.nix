{
  lib,
  pkg-config,
  rustPlatform,
  sqlite,
  src,
}:

rustPlatform.buildRustPackage {
  pname = "token-proxy";
  version = "0.1.175";

  inherit src;

  patches = [
    ./startup-sqlite-order.patch
    ./model-alias-content-length.patch
  ];

  cargoLock.lockFile = "${src}/Cargo.lock";
  cargoBuildFlags = [ "-p" "token_proxy_cli" ];
  # The executable spike below is the package-level check. Upstream's workspace
  # tests are intentionally not rebuilt as part of every Nix package build.
  doCheck = false;

  nativeBuildInputs = [ pkg-config ];
  buildInputs = [ sqlite ];

  meta = {
    description = "Headless token_proxy OpenAI-compatible gateway";
    homepage = "https://github.com/mxyhi/token_proxy";
    license = lib.licenses.asl20;
    mainProgram = "token-proxy";
    platforms = lib.platforms.unix;
  };
}
