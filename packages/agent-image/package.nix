{
  cacert,
  coreutils,
  dockerTools,
  git,
  hydra-acp,
  jq,
  lib,
  pi,
  pi-acp,
  pi-tool-profile,
  writeShellApplication,
}:

let
  bootstrap = writeShellApplication {
    name = "agent-runtime-bootstrap";
    runtimeInputs = [
      coreutils
      git
      hydra-acp
      jq
      pi-acp
      pi
      pi-tool-profile
    ];
    text = builtins.readFile ./bootstrap.sh;
  };
in
dockerTools.buildLayeredImage {
  name = "lattice-agent-runtime";
  tag = "latest";
  created = "1970-01-01T00:00:01Z";
  contents = [
    bootstrap
    cacert
    dockerTools.caCertificates
    dockerTools.fakeNss
    git
    hydra-acp
    pi
    pi-acp
    pi-tool-profile
  ];
  extraCommands = ''
    mkdir -m 1777 -p tmp
    mkdir -m 0755 -p workspace run root etc/ssl/certs
  '';
  config = {
    Entrypoint = [ "${bootstrap}/bin/agent-runtime-bootstrap" ];
    WorkingDir = "/workspace";
    Volumes = {
      "/workspace" = { };
    };
    Env = [
      "PATH=${lib.makeBinPath [ pi-tool-profile pi pi-acp hydra-acp bootstrap ]}:/bin:/usr/bin"
      "SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt"
      "LANG=C.UTF-8"
      "LC_ALL=C.UTF-8"
    ];
    ExposedPorts = {
      "55514/tcp" = { };
    };
  };
  passthru = {
    inherit bootstrap;
  };
}
