{ fetchurl, ffmpeg-headless, llama-cpp, stdenv, writeShellScriptBin, writeText }:
let
  revision = "6ea3e3a309d12a4779cfda98b8c19800d2d5da71";
  weights = name: sha256: fetchurl {
    url = "https://huggingface.co/ggml-org/embeddinggemma-2-GGUF/resolve/${revision}/${name}";
    inherit sha256;
  };
  model = weights "embeddinggemma-2-BF16.gguf"
    "68bae29d62fb8c7d23e98d21fd4662753ddd636e6b62b8a70dfe059a9844f216";
  mmproj = weights "mmproj-embeddinggemma-2-BF16.gguf"
    "cc8d7eb30a9984c5128d6b1ccdec7510bfce213dc92a840be0d20e2a62b26bcd";
  preset = writeText "embeddinggemma-2.ini" ''
    [google/embeddinggemma-2]
    model = ${model}
    mmproj = ${mmproj}
    embedding = true
    load-on-startup = false
    n-gpu-layers = ${if stdenv.hostPlatform.isDarwin then "all" else "0"}
    parallel = 1
    ctx-size = 8192
    batch-size = 8192
    ubatch-size = 8192
    video-ffmpeg-dir = ${ffmpeg-headless}/bin
  '';
in
writeShellScriptBin "llama-embeddings" ''
  exec ${llama-cpp}/bin/llama-server \
    --host 127.0.0.1 --port 9226 \
    --models-preset ${preset} --models-max 1 \
    --sleep-idle-seconds 1200 --metrics --no-webui "$@"
''
