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
  version = if bin == "rnsh" then "0.2.4-unstable-2026-05-25" else "0.1.3-unstable-2026-05-25";

  src = fetchFromGitHub {
    owner = "lelloman";
    repo = "rns-rs";
    rev = "7743a77938a03defbbace03913117c03db18ccdf";
    hash = "sha256-Ok9IDkVCVDPxBd3UoDjpJ+qKGg4bA46k4vf87fcDpxM=";
  };

  cargoHash = "sha256-odO4pjMgphjOMiohu13oikB8uHacJCl5Bsfu+ebI4Gc=";

  postPatch = ''
    substituteInPlace rns-cli/src/rnsh.rs \
      --replace-fail "std::ptr::null()," "std::ptr::null_mut()," \
      --replace-fail "libc::ioctl(tty_fd, libc::TIOCSCTTY, 0);" "libc::ioctl(tty_fd, libc::TIOCSCTTY.into(), 0);"
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
