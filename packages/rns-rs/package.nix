{ lib
, rustPlatform
, fetchFromGitHub
, pkg-config
, openssl
, coreutils
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
    rev = "042e37047b70ea0e06b9aff0aed6214bc305ab35";
    hash = "sha256-cwei72TfRBlk5fEvbLyIxhRSef8Zy9GkrZmj667EjMA=";
  };

  cargoHash = "sha256-cWUs8ZQEhYwjwHPTP2lA3BxbtH49KRs1wQjwygwmtPY=";

  patches = lib.optionals (bin == "rns-server") (
    [ ./shared-local-delivery.patch ]
    ++ lib.optionals stdenv.hostPlatform.isDarwin [ ./darwin-local-client.patch ]
  );

  # GitHub source archives have no .git directory. Keep CLI versions traceable
  # without deriving them from an unavailable Git commit count.
  env.RNS_BUILD_REV = builtins.substring 0 12 src.rev;

  postPatch = ''
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

  doCheck = true;
  cargoTestFlags = if bin == "rnsh"
    then [ "-p" "rns-cli" "--lib" "rnsh::tests::" ]
    else [ "-p" "rns-core" "--lib" ];

  # The upstream process test uses /bin/cat, absent in the Nix Linux sandbox.
  preCheck = lib.optionalString (bin == "rnsh") ''
    substituteInPlace rns-cli/src/rnsh.rs \
      --replace-fail '"/bin/cat"' '"${lib.getExe' coreutils "cat"}"'
  '';

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
