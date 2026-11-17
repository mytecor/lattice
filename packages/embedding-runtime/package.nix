{ lib, python3Packages, makeWrapper, ffmpeg, callPackage }:
python3Packages.buildPythonApplication {
  pname = "lattice-embedding-runtime";
  version = "0.1.0";
  pyproject = true;
  src = lib.fileset.toSource {
    root = ./.;
    fileset = lib.fileset.unions [ ./pyproject.toml
      (lib.fileset.fileFilter (file: file.hasExt "py") ./embedding_runtime)
      (lib.fileset.fileFilter (file: file.hasExt "py") ./tests)
    ];
  };
  build-system = [ python3Packages.setuptools ];
  dependencies = with python3Packages; [
    aiohttp sentence-transformers transformers torch numpy pillow
    av librosa soundfile torchvision torchcodec
  ];
  nativeBuildInputs = [ makeWrapper ];
  nativeCheckInputs = [ ffmpeg ];
  postFixup = ''
    for binary in $out/bin/embedding-runtime $out/bin/embedding-runtime-benchmark; do
      wrapProgram "$binary" --prefix PATH : ${lib.makeBinPath [ ffmpeg ]}
    done
  '';
  checkPhase = ''
    runHook preCheck
    python -m unittest discover -s tests -v
    runHook postCheck
  '';
  passthru.weights = callPackage ./weights.nix { };
  meta = {
    description = "Offline multimodal embeddings with a disposable CPU inference worker";
    mainProgram = "embedding-runtime";
    license = lib.licenses.mit;
    platforms = lib.platforms.unix;
  };
}
