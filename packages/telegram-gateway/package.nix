{
  lib,
  go,
  buildGoModule,
  fetchFromGitHub,
}:
(buildGoModule.override { inherit go; }) {
  pname = "telegram-gateway";
  version = "unstable-2026-11-27";

  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "telegram-gateway";
    rev = "c7702c99251b51f10fb53019605650ecb4bfa744";
    hash = "sha256-S9fjMrx7F5VWyex36AglHuFDbuM+vNbsG9x1g1rvbGo=";
  };

  vendorHash = "sha256-MqCYCHimc5T+Ph/P9x4FFPJUOIXiF2pJGFR3iiq8R6k=";
  subPackages = [ "cmd/telegram-gateway" ];
  env.GOMEMLIMIT = "2100MiB";
  enableParallelBuilding = false;

  meta = {
    description = "Telegram history, media streaming, and an HTTP event journal";
    homepage = "https://github.com/mytecor/telegram-gateway";
    mainProgram = "telegram-gateway";
    platforms = lib.platforms.linux;
  };
}
