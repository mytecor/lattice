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
    rev = "e4e382e7ce45bd0246bc91892c4adfa68dfa98aa";
    hash = "sha256-9Z7J2R380X32jvbFsDQwHjgJVp0g7eFfw51jj0VmM2A=";
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
