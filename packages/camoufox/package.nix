# F18: Camoufox — anti-detect Firefox build (binary distribution).
# Downloaded as the official GitHub release zip and auto-patched for the
# Nix store (autoPatchelfHook), following the nixpkgs `firefox-bin` pattern.
# Foxbridge launches the `camoufox` launcher directly with
# `--juggler-pipe --headless`; no Python `camoufox` package is needed at
# runtime (that package only *fetches* the browser — the flake pins the exact
# release instead, so the browser becomes reproducible).
#
# Version pinned to 152.0.4-beta.30 (verified in f18-01..f18-07 smoke runs on
# mytecor-homelab). The zip sha256 comes from the Camoufox GitHub release
# assets (repo_cache.json in the working /root/.cache/camoufox).
{ lib, stdenv, fetchurl, unzip, autoPatchelfHook, patchelfUnstable, alsa-lib, curl, dbus-glib, gtk3, libxtst, libva, pciutils, pipewire, ffmpeg_8, libglvnd, mesa }:

let
  version = "152.0.4-beta.30";
in
stdenv.mkDerivation {
  pname = "camoufox";
  inherit version;

  src = fetchurl {
    url = "https://github.com/daijro/camoufox/releases/download/v${version}/camoufox-${version}-lin.x86_64.zip";
    sha256 = "sha256-VyDUW4lM4XcFQ94CTG8Q1RSzi+Vg+i3DIms9hYbK9nI=";
  };

  nativeBuildInputs = [ unzip autoPatchelfHook patchelfUnstable ];
  # Firefox uses "relrhack" — it manually processes relocations from a fixed
  # offset during startup. autoPatchelf must not clobber the old sections when
  # rewriting the interpreter/rpath, or NSPR init SEGVs. Exact same flag + 
  # patchelfUnstable pairing as nixpkgs firefox-bin (patchelfUnstable accepts
  # the flag after the file arg, unlike the stable 0.15.2).
  patchelfFlags = [ "--no-clobber-old-sections" ];
  buildInputs = [
    gtk3
    alsa-lib
    dbus-glib
    libxtst
  ];
  runtimeDependencies = [
    curl.out
    pciutils
    libva.out
  ];
  # dlopen is called from libxul, not just the launcher. DT_RUNPATH on the
  # executable is not inherited by shared-library callers, so these paths
  # must also be present on the browser's shared objects.
  # Firefox 152 supports libavcodec 62 (FFmpeg 8), not an arbitrary newer ABI.
  appendRunpaths = map (package: "${package}/lib") [
    pipewire
    (lib.getLib ffmpeg_8)
    libglvnd
    mesa
  ];

  dontConfigure = true;
  dontBuild = true;
  dontStrip = true;
  # The release zip is FLAT: ELF `camoufox`, libmoz*.so and dependentlibs.list
  # sit at the archive root next to application.ini — there is no single
  # top-level dir, so the default unpackPhase errors with "produced multiple
  # directories". Skip unpack/build entirely and unzip straight into $out/lib,
  # preserving the flat layout Firefox needs (dependentlibs.list is relative to
  # the executable's directory).
  dontUnpack = true;

  installPhase = ''
    runHook preInstall
    mkdir -p "$out/lib/camoufox-${version}"
    unzip -qq "$src" -d "$out/lib/camoufox-${version}"
    # The Python launcher normally enables WebGL explicitly. Raw binary
    # launches through Foxbridge need the same defaults for software Mesa.
    cat >> "$out/lib/camoufox-${version}/camoufox.cfg" <<'PREFS'
    defaultPref("webgl.force-enabled", true);
    defaultPref("webgl.enable-webgl2", true);
    PREFS
    runHook postInstall
  '';

  postFixup = ''
    mkdir -p "$out/bin"
    # `camoufox` and `camoufox-bin` are the identical Firefox ELF launcher. A
    # store symlink is enough: the binary locates libs by its own path
    # (independentlibs relative to /proc/self/exe`, which resolves through the
    # symlink to the real lib dir) — same approach as nixpkgs firefox-bin.
    ln -s "$out/lib/camoufox-$version/camoufox" "$out/bin/camoufox"
  '';

  meta = {
    description = "Anti-detect Firefox build for browser automation (F18)";
    homepage = "https://github.com/daijro/camoufox";
    license = lib.licenses.mpl20;
    platforms = [ "x86_64-linux" ];
    mainProgram = "camoufox";
  };
}
