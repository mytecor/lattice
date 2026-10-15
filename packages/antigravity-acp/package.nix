{ lib
, stdenv
, fetchurl
, unzip
, nodejs
, coreutils
, writeShellApplication
, autoPatchelfHook ? null
}:

let
  version = "1.4.0";

  sources = {
    x86_64-linux = {
      url = "https://dl.google.com/agy-extensions/releases/linux/agy-acp-server-${version}-linux-x86_64.zip";
      sha256 = "fbb2d54dd94fb897845308d3b6b46187c627f1c60078ded81adeeaf8b90c5d7a";
    };
    aarch64-darwin = {
      url = "https://dl.google.com/agy-extensions/releases/macos/agy-acp-server-${version}-darwin-arm64.zip";
      sha256 = "21a32eeab85b394a515510c84539bda4b2504bf2917e0a201f5c4130b13581a5";
    };
  };

  srcInfo = sources.${stdenv.hostPlatform.system} or (throw "antigravity-acp: unsupported platform ${stdenv.hostPlatform.system}");

  adapter = stdenv.mkDerivation {
    pname = "antigravity-acp";
    inherit version;

    src = fetchurl {
      inherit (srcInfo) url sha256;
    };

    sourceRoot = ".";

    nativeBuildInputs = [ unzip ] ++ lib.optionals stdenv.hostPlatform.isLinux (lib.optional (autoPatchelfHook != null) autoPatchelfHook);
    buildInputs = lib.optionals stdenv.hostPlatform.isLinux [ stdenv.cc.cc.lib ];

    installPhase = ''
      runHook preInstall
      mkdir -p "$out/libexec/antigravity-acp" "$out/bin"
      install -m 0755 agy_acp_server.par "$out/libexec/antigravity-acp/agy_acp_server.par"
      install -m 0755 localharness_external "$out/libexec/antigravity-acp/localharness_external"

      cat > "$out/bin/antigravity-acp" <<'EOF'
      #!/usr/bin/env bash
      set -euo pipefail
      export ANTIGRAVITY_HARNESS_PATH="''${ANTIGRAVITY_HARNESS_PATH:-${placeholder "out"}/libexec/antigravity-acp/localharness_external}"
      exec "${placeholder "out"}/libexec/antigravity-acp/agy_acp_server.par" ${lib.optionalString stdenv.hostPlatform.isLinux "--uid="} "$@"
      EOF
      chmod +x "$out/bin/antigravity-acp"
      runHook postInstall
    '';

    meta = {
      description = "Google Antigravity ACP server";
      homepage = "https://antigravity.google/docs/ide/extensions";
      license = lib.licenses.unfree;
      mainProgram = "antigravity-acp";
      platforms = builtins.attrNames sources;
    };
  };
in
writeShellApplication {
  name = "antigravity-acp-fsless";
  runtimeInputs = [ coreutils nodejs ];
  text = ''
    : "''${GEMINI_HOME:?a dedicated GEMINI_HOME is required}"
    mkdir -p "$GEMINI_HOME/antigravity-acp"
    chmod 0700 "$GEMINI_HOME" "$GEMINI_HOME/antigravity-acp"

    # Seed minimal settings.json if missing so auth.type is recognized eagerly
    if [ ! -f "$GEMINI_HOME/antigravity-acp/settings.json" ]; then
      if [ -n "''${AGY_LLM_GATEWAY_URL:-}" ]; then
        auth_type="gateway"
      elif [ -n "''${GOOGLE_API_KEY:-}" ] || [ -n "''${GOOGLE_CLOUD_PROJECT:-}" ]; then
        auth_type="agent-platform"
      else
        auth_type="gemini-api-key"
      fi
      cat > "$GEMINI_HOME/antigravity-acp/settings.json" <<EOF
{"auth":{"type":"$auth_type"}}
EOF
      chmod 0600 "$GEMINI_HOME/antigravity-acp/settings.json"
    fi

    exec ${lib.getExe nodejs} ${./filter.mjs} ${lib.getExe adapter} "$@"
  '';
  passthru = { inherit adapter; };
}
