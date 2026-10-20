{ lib, buildGoModule }:
buildGoModule {
  pname = "browser-ingress";
  version = "0.1.0";
  src = ./.;
  vendorHash = null;
  meta = {
    description = "Shared DevTools frontend and CDP reverse proxy for Moli and Foxbridge";
    license = lib.licenses.mit;
    mainProgram = "browser-ingress";
    platforms = lib.platforms.linux;
  };
}
