# Exact upstream files; this fixed-output snapshot is used offline at runtime.
{ runCommand, fetchurl }:
let
  revision = "f9972ac22e696a105c482202646979e33e4b5a1e";
  hashes = {
    "1_Pooling/config.json" = "sha256-h1m998d+/H33cj9khWpZPIlDtx7ji68qiHcfuveEOPk=";
    "2_Normalize/config.json" = "sha256-zbCd/KNHpWqi1pF0TjjVrTx8vCg05xgScrmhUyi4JSQ=";
    "chat_template.jinja" = "sha256-S4Uu/AuZYCg+c1NjMx5vMlszvHS9uqB29ZW8TpuU2F4=";
    "config.json" = "sha256-uPHpkxtX+8BUrNtEXEF2XVWwB0xY0UX6goOZQa0bW7M=";
    "config_sentence_transformers.json" = "sha256-Ax5WpJjTPDSatImiGIW8/iW0/LqEEUncmeHpDUp8KPU=";
    "model.safetensors" = "sha256-GXoyll1LEQX68GBBe6qJnhk/tzzUAfQuySlSNNVVPXk=";
    "modules.json" = "sha256-PQJXKgRVuDLeZ/uOY6VJgbx+i0bjN8lekXvYEipTO/0=";
    "preprocessor_config.json" = "sha256-6iriV+kBBkq92Y3OsZ8rDaBq9gC+0V4PmfXIXDfunXg=";
    "processor_config.json" = "sha256-Fo9qCFIvPOXepZbZTQA68v1pF0LU9B/h+djM52v79pw=";
    "sentence_bert_config.json" = "sha256-sbzZ8tzjroY7NZ6H0HELXbwzFKWey04vl8d3j8jksig=";
    "tokenizer.json" = "sha256-TXd+9b3BqjYier37d8PknnuciS0W4ba9pBw5NQSCi+Q=";
    "tokenizer.model" = "sha256-5ZTIqQ6wjYvaSY/0dHl33IJ64MPFa1wNQaYFoi0C7wM=";
    "tokenizer_config.json" = "sha256-F71dbpNkykmlNOFQIHZZMxfCmNSmY2Iwke1FOI8ASHQ=";
  };
  files = builtins.mapAttrs (name: hash: fetchurl {
    url = "https://huggingface.co/google/embeddinggemma-2/resolve/${revision}/${name}";
    inherit hash;
    name = "embeddinggemma-2-${builtins.replaceStrings [ "/" ] [ "-" ] name}";
  }) hashes;
in runCommand "embeddinggemma-2-${builtins.substring 0 12 revision}" {
  passthru = { inherit revision; modelId = "google/embeddinggemma-2"; };
} ("mkdir -p $out\n" + builtins.concatStringsSep "\n" (builtins.attrValues
  (builtins.mapAttrs (name: file: ''
    mkdir -p "$out/$(dirname '${name}')"
    ln -s ${file} "$out/${name}"
  '') files)))
