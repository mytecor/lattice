{ lib, buildGoModule }:
buildGoModule {
  pname = "moli-ingress";
  version = "0.1.0";
  src = ./.;
  vendorHash = null;
  meta = {
    description = "CDP discovery and WebSocket reverse proxy for Moli";
    license = lib.licenses.mit;
    mainProgram = "moli-ingress";
    platforms = lib.platforms.linux;
  };
}
