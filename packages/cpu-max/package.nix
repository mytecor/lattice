{ lib, stdenv, fetchFromGitHub, fetchurl, ffmpeg-headless, makeWrapper
, python3, writeShellScript
}:
let
  revision = "197f8fbadd59ffe68e18e90892a1c4bdb6cf9c60";
  weightRevision = "6ea3e3a309d12a4779cfda98b8c19800d2d5da71";
  weight = name: sha256: fetchurl {
    url = "https://huggingface.co/ggml-org/embeddinggemma-2-GGUF/resolve/${weightRevision}/${name}";
    inherit sha256;
  };
  model = weight "embeddinggemma-2-Q8_0.gguf"
    "2188ac1deca4b77dffefd603c2776a9d76d9d74ec01841392982ebb840b09135";
  mmproj = weight "mmproj-embeddinggemma-2-Q8_0.gguf"
    "0531309472a6bc6f4b146c3736eee7795ec9db1903c25490d3c937490cef6dd8";
  python = python3.withPackages (ps: [ ps.pillow ]);
  media = writeShellScript "cpu-max-prepare-media" ''
    exec ${python}/bin/python3 ${./media.py} "$@"
  '';
in stdenv.mkDerivation {
  pname = "embeddinggemma-cpu-max";
  version = "0.1.0-unstable";
  src = fetchFromGitHub {
    owner = "Yazeedx0";
    repo = "embeddinggemma-cpu-max";
    rev = revision;
    hash = "sha256-P5N2LzDJWqiUlLSdMYYACc9wP2hchZPxFYGnRUCP0aQ=";
  };
  patches = [ ./public-api.patch ];
  nativeBuildInputs = [ makeWrapper ];
  makeFlags = [ "GIT_COMMIT=${builtins.substring 0 7 revision}" "build/embeddinggemma" ];
  installPhase = ''
    runHook preInstall
    install -Dm755 build/embeddinggemma "$out/libexec/embeddinggemma"
    makeWrapper "$out/libexec/embeddinggemma" "$out/bin/cpu-max" \
      --set EI_GEMMA2_SEMANTICS official \
      --set EI_LATTICE_MEDIA_HELPER ${media} \
      --prefix PATH : ${lib.makeBinPath [ ffmpeg-headless ]} \
      --add-flags "--bind 127.0.0.1 --backend cpu --model ${model} --mmproj ${mmproj}"
    runHook postInstall
  '';
  meta = {
    description = "CPU-Max EmbeddingGemma 2 with inline multimodal embeddings over HTTP";
    homepage = "https://github.com/Yazeedx0/embeddinggemma-cpu-max";
    license = lib.licenses.mit;
    mainProgram = "cpu-max";
    platforms = [ "x86_64-linux" ];
  };
}
