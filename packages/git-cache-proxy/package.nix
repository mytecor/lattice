{ lib
, rustPlatform
, fetchFromGitHub
, git
, makeWrapper
, stdenv
}:

rustPlatform.buildRustPackage rec {
  pname = "git-cache-proxy";
  version = "0.1.12";

  src = fetchFromGitHub {
    owner = "rolandjitsu";
    repo = "git-cache-proxy";
    rev = "v${version}";
    hash = "sha256-iq9/3Yjiw2mucs4PYIoBXQEwN6dsbBfz8WZHlwK4WTs=";
  };

  cargoHash = "sha256-CtT7wCWOa8bzW/mmXzPM3YTqHgzmFKSmQp7QXJux41Y=";

  # Lattice patch (f9-02): repo-scoped authorization allowlist. Upstream 0.1.12
  # has a single global serve token and no way to restrict *which* repositories
  # a request may target, so one upstream credential reads everything it can
  # reach. This patch adds `--allow-repo` (repeatable): every serving path (git
  # info/refs, upload-pack, LFS batch, LFS object) refuses a repository outside
  # the list with 404 before any upstream fetch or cache read — even when the
  # mirror is already materialized. See packages/git-cache-proxy/README.md and
  # docs/roadmap/f9-cache-artifact-plane/f9-02-git-repository-access.md.
  patches = [ ./repo-allowlist.patch ];

  # The proxy delegates all git wire-protocol work to the system `git` binary
  # (clone --mirror / fetch / upload-pack --stateless-rpc). makeWrapper keeps a
  # pinned git on PATH at runtime; git in nativeBuildInputs also satisfies the
  # e2e test suite (spawns `git` during `cargo test`).
  nativeBuildInputs = [ makeWrapper git ];
  buildInputs = [ ];

  postInstall = ''
    wrapProgram $out/bin/git-cache-proxy \
      --prefix PATH : ${lib.makeBinPath [ git ]}
  '';

  meta = with lib; {
    description = "Read-only caching proxy for Git repositories";
    longDescription = ''
      Serves clones/fetches from a local bare mirror and pulls only the delta
      from upstream on each request. Read-only and pull-only: it never pushes
      upstream, and only copies what a client requested.
    '';
    homepage = "https://github.com/rolandjitsu/git-cache-proxy";
    license = licenses.asl20;
    mainProgram = "git-cache-proxy";
  };
}
