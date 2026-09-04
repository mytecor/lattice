{ lib
, rustPlatform
, fetchFromGitHub
, pkg-config
, openssl
, stdenv
, bin ? "rns-server"
}:

let
  crate = {
    rns-server = "rns-server";
    rnsh = "rns-cli";
  }.${bin} or (throw "unsupported rns-rs binary: ${bin}");
in
rustPlatform.buildRustPackage rec {
  pname = bin;
  version = if bin == "rnsh" then "0.4.1-unstable-2026-09-04" else "0.3.1-unstable-2026-09-04";

  src = fetchFromGitHub {
    owner = "lelloman";
    repo = "rns-rs";
    rev = "cb257acf53eb4630a9faa62b3dd6d8475a7f0df0";
    hash = "sha256-4mJ+AttnbQCAj+mLKuubTzb0NpAV/Zic2Jpj2z2FH9s=";
  };

  cargoHash = "sha256-cWUs8ZQEhYwjwHPTP2lA3BxbtH49KRs1wQjwygwmtPY=";

  # GitHub source archives have no .git directory. Keep CLI versions traceable
  # without deriving them from an unavailable Git commit count.
  env.RNS_BUILD_REV = builtins.substring 0 12 src.rev;

  postPatch = ''
    substituteInPlace rns-cli/src/rnsh.rs \
      --replace-fail "std::ptr::null()," "std::ptr::null_mut()," \
      --replace-fail "libc::ioctl(tty_fd, libc::TIOCSCTTY, 0);" "libc::ioctl(tty_fd, libc::TIOCSCTTY.into(), 0);"

    substituteInPlace rns-cli/build_common.rs rns-server/build_common.rs build/common.rs \
      --replace-fail 'pub fn emit_full_version() {' 'pub fn emit_full_version() {
        if let Ok(rev) = std::env::var("RNS_BUILD_REV") {
            println!("cargo:rerun-if-env-changed=RNS_BUILD_REV");
            println!("cargo:rustc-env=FULL_VERSION={}-{}", env!("CARGO_PKG_VERSION"), rev);
            return;
        }'
  '';

  cargoBuildFlags = [
    "-p"
    crate
    "--bin"
    bin
  ];

  doCheck = false;

  installPhase = ''
    runHook preInstall

    install -Dm755 target/${stdenv.hostPlatform.rust.rustcTarget}/release/${bin} $out/bin/${bin}

    runHook postInstall
  '';

  nativeBuildInputs = [ pkg-config ];
  buildInputs = [ openssl ];

  meta = {
    description = if bin == "rnsh" then "Reticulum remote shell utility" else "Batteries-included Reticulum node server";
    homepage = "https://github.com/lelloman/rns-rs";
    license = lib.licenses.unfree;
    mainProgram = bin;
  };
}
