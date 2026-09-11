{ lib, stdenv, nodejs }:

# Lattice-owned hydra-acp transformer: normalize per-token `messageId` into one
# stable `messageId` per logical assistant message before the daemon broadcasts
# `session/update` to ACP clients (see ./README.md). Dependency-free: uses only
# Node built-ins (global `WebSocket`, `crypto.randomUUID`), so no pnpm build is
# needed — a plain install + `patchShebangs` against the pinned Node is enough.
stdenv.mkDerivation {
  pname = "acp-normalizer";
  version = "0.1.0";

  src = lib.cleanSource ./.;

  nativeBuildInputs = [ ];
  buildInputs = [ nodejs ];

  dontConfigure = true;
  dontBuild = true;

  installPhase = ''
    runHook preInstall
    mkdir -p $out/bin
    install -m 0755 $src/acp-normalizer.mjs $out/bin/acp-normalizer
    patchShebangs $out/bin/acp-normalizer
    runHook postInstall
  '';

  meta = {
    description = "Hydra ACP transformer that reassigns per-token messageId onto a stable per-message id";
    longDescription = ''
      hydra-acp upstream streams every text chunk of one assistant reply as a
      separate session/update carrying its own fresh messageId. Clients that key
      rendering on messageId (e.g. superlite) then display each chunk as its own
      message. This transformer runs in the response:session/update chain before
      the daemon broadcast and re-stamps all chunks of one logical assistant
      message with the messageId of the first chunk (or a minted id when the
      agent streams without one). A "strip messageId" approach cannot work:
      hydra's recordAndBroadcast re-injects a fresh messageId into any recordable
      update that lacks one after the transform chain.
    '';
    license = lib.licenses.mit;
    mainProgram = "acp-normalizer";
  };
}
