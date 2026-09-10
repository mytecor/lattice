{
  fetchPnpmDeps,
  lib,
  makeWrapper,
  nodejs,
  pnpm,
  pnpmConfigHook,
  runCommand,
  stdenvNoCC,
  writeText,
}:

{
  executables,
  package,
  pname,
  pnpmDepsHash,
  pnpmLock,
  version,
  description,
  homepage,
  license ? lib.licenses.mit,
  mainProgram ? builtins.head (builtins.attrNames executables),
  nodePackage ? nodejs,
  pnpmPackage ? pnpm,
  versionCheck ? true,
  versionCheckArgs ? [ "--version" ],
  versionCheckOutput ? version,
}:

let
  packageJson = writeText "${pname}-package.json" (builtins.toJSON {
    name = "nix-${pname}";
    private = true;
    version = "0.0.0";
    packageManager = "pnpm@${pnpmPackage.version}";
    dependencies.${package} = version;
  });

  source = runCommand "${pname}-${version}-pnpm-source" { } ''
    mkdir -p "$out"
    cp ${packageJson} "$out/package.json"
    cp ${pnpmLock} "$out/pnpm-lock.yaml"
  '';

  wrappers = lib.concatStringsSep "\n" (lib.mapAttrsToList
    (name: entrypoint: ''
      makeWrapper ${lib.getExe nodePackage} "$out/bin/${name}" \
        --add-flags "$out/libexec/${pname}/node_modules/${package}/${entrypoint}"
    '')
    executables);
in
stdenvNoCC.mkDerivation (finalAttrs: {
  inherit pname version;
  src = source;

  pnpmDeps = fetchPnpmDeps {
    inherit (finalAttrs) pname src version;
    pnpm = pnpmPackage;
    fetcherVersion = 4;
    hash = pnpmDepsHash;
  };

  nativeBuildInputs = [
    makeWrapper
    nodePackage
    pnpmConfigHook
    pnpmPackage
  ];

  dontBuild = true;
  strictDeps = true;

  installPhase = ''
    runHook preInstall

    mkdir -p "$out/bin" "$out/libexec/${pname}"
    cp package.json pnpm-lock.yaml "$out/libexec/${pname}/"
    cp -R node_modules "$out/libexec/${pname}/"
    ${wrappers}

    runHook postInstall
  '';

  doInstallCheck = versionCheck;
  installCheckPhase = ''
    runHook preInstallCheck

    test "$($out/bin/${mainProgram} ${lib.escapeShellArgs versionCheckArgs})" = \
      ${lib.escapeShellArg versionCheckOutput}

    runHook postInstallCheck
  '';

  passthru = {
    inherit package packageJson pnpmLock source;
  };

  meta = {
    inherit description homepage license mainProgram;
    platforms = nodePackage.meta.platforms;
  };
})
