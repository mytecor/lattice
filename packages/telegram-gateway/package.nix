{
  lib,
  go,
  buildGoModule,
  fetchFromGitHub,
}:
(buildGoModule.override { inherit go; }) {
  pname = "telegram-gateway";
  version = "unstable-2026-11-26";

  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "telegram-gateway";
    rev = "58900a5cb4462513c9ba11a513c5a1ba3b15e7ea";
    hash = "sha256-6amVdbfCZ98XM7GYmi0DjtS4FweffZDisfRfvDmAvE8=";
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
