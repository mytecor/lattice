# F18: Jev UltraFast — browser agent runtime (upstream browser-use/jev-ultrafast).
#
# This is a self-contained Python derivation that builds the original
# `jev` console script with its exact pinned dependency set:
#   - jev-ultrafast 0.1.0 (upstream, kept byte-for-byte — snapshot.js/policy untouched)
#   - browser-harness 0.1.13 (pinned by jev; the CDP harness daemon)
#   - cdp-use 1.4.5 and fetch-use 0.4.0 (transitive pins of browser-harness)
#   - websockets 15.0.1 (hard pin from browser-harness; nixpkgs has 16.1, so
#     this exact version is built here as an override)
#
# The `jev` script expects `buf`/http2 + a running `browser-harness` daemon; the
# systemd module wires those up (BU_CDP_URL / BH_RUNTIME_DIR). No Jev policy or
# snapshot.js is modified — the tarball is used as-is.
{ lib, fetchurl, fetchFromGitHub, python3 }:

let
  # Exact sdist hashes pinned from PyPI (verified 2026-09-22, matches the
  # working venv on mytecor-homelab).
  py3 = python3.override { packageOverrides = self: super: {
    # browser-harness hard-pins websockets==15.0.1; nixpkgs 26.11 ships 16.1.
    # Build 15.0.1 as a fresh derivation (pure-Python, setuptools backend) so
    # the pinned version replaces the 16.1 one for this runtime only.
    websockets = self.buildPythonPackage rec {
      pname = "websockets";
      version = "15.0.1";
      format = "pyproject";
      src = fetchurl {
        url = "https://files.pythonhosted.org/packages/source/w/websockets/websockets-15.0.1.tar.gz";
        sha256 = "sha256-glRN4CB2uvugOM4FXuZBLWjaE6tH8MYMq4JzRt6Cje4=";
      };
      doCheck = false;
    };
    ## cdp-use 1.4.5 (not in nixpkgs)
    cdp-use = self.buildPythonPackage rec {
      pname = "cdp-use";
      version = "1.4.5";
      format = "pyproject";
      src = fetchurl {
        url = "https://files.pythonhosted.org/packages/source/c/cdp-use/cdp_use-1.4.5.tar.gz";
        sha256 = "sha256-DaOjLfRjNqA/9aIrxrxELNfS8tUKEY/UhW8p039tJqA=";
      };
      propagatedBuildInputs = with self; [ httpx typing-extensions websockets ];
      doCheck = false;
    };
    ## fetch-use 0.4.0 (not in nixpkgs)
    fetch-use = self.buildPythonPackage rec {
      pname = "fetch-use";
      version = "0.4.0";
      format = "pyproject";
      src = fetchurl {
        url = "https://files.pythonhosted.org/packages/source/f/fetch-use/fetch_use-0.4.0.tar.gz";
        sha256 = "sha256-lRGYfUkH7G2sUB4h1mlG0QCY9mtdIbwqukGJzYG6GJo=";
      };
      propagatedBuildInputs = [ ];
      doCheck = false;
    };
    ## browser-harness 0.1.13 (not in nixpkgs)
    browser-harness = self.buildPythonPackage rec {
      pname = "browser-harness";
      version = "0.1.13";
      format = "pyproject";
      src = fetchurl {
        url = "https://files.pythonhosted.org/packages/source/b/browser-harness/browser_harness-0.1.13.tar.gz";
        sha256 = "sha256-KE3FR6BCwwn+r9mp9KdLKoZRt5Y+o6xssvLWSIn2qPM=";
      };
      propagatedBuildInputs = with self; [ cdp-use fetch-use pillow websockets ];
      doCheck = false;
    };
  }; };
in
py3.pkgs.buildPythonPackage.override { python = py3; } rec {
  pname = "jev-ultrafast";
  version = "0.1.0";
  format = "pyproject";

  src = fetchFromGitHub {
    owner = "browser-use";
    repo = "jev-ultrafast";
    rev = "1231850a0bf1a0c0341fe408ef1668dbbfdfac46";
    hash = "sha256-8EJhsOjalxX6uUCu+bREqopVUBG8O64SehhQUdNUwVI=";
  };

  nativeBuildInputs = [ py3.pkgs.hatchling ];
  propagatedBuildInputs = with py3.pkgs; [
    browser-harness
    httpx
    h2
    pillow
  ];

  doCheck = false;

  meta = {
    description = "Upstream browser-use/jev-ultrafast browser agent (F18)";
    homepage = "https://github.com/browser-use/jev-ultrafast";
    license = with lib.licenses; [ mit ];
    mainProgram = "jev";
    platforms = lib.platforms.linux;
  };
}
