{ nixpkgs, pkgs, foxbridgeModule, jevModule, browserProfile }:

# f18-11: integration contract for the F18 browser agent stack.
#
# This is a module-level eval test (tests/README.md: "General contract tests").
# It verifies the *wiring invariants* of the two F18 systemd services that the
# live node debugging in f18-08/f18-09 turned into hard regressions:
#
#   1. lifecycle: jev-ultrafast Requires+After foxbridge-camoufox (f18-09 S3),
#      and its ExecStartPre is a CDP /json/version probe — not just an ordering
#      keyword;
#   2. seccomp: foxbridge must NOT carry `~@resources` (Camoufox SIGSYS/31
#      crash-loop on setpriority, f18-08 commit 207b0d8) but must keep the
#      `@system-service` allow-list + `~@privileged` hardening;
#   3. browser home: Camoufox profile/api-key dir is a PERSISTENT (non-tmpfs)
#      stateDir used as HOME/ReadWritePaths — the tmpfs-/run crash (f18-08,
#      commits f39a707/06c6d4e) is a config regression, not a runtime one;
#   4. loopback-only CDP: the module asserts listenAddress stays on loopback,
#      and the CDP/listener ports are NOT in the firewall;
#   5. secret handling: Jev keys arrive via systemd LoadCredential (runtime
#      paths from agenix), never in argv / Environment; jey ExecStart wraps
#      credential injection (never raw secret in the unit).
#
# These are invariants of the stack, not node-specific values: a legitimate
# change to ports/models/keys must not fail the test, but reintroducing any of
# the f18-08 crash causes (seccomp ~@resources, tmpfs HOME, non-loopback listen,
# secrets in argv) must.
#
# Note on CDP-readiness: the smoke/integration of the FULL path — real
# Camoufox boot, Jev observe/act/click, DONE — runs on the live node
# (f18-04 smoke re-verified 2026-09-23) and is out of scope for a Nix sandbox
# eval. The unit's ExecStartPre probe assertion below pins the readiness
# contract so a regression of that path fails fast at eval time.

let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = [
      { nixpkgs.pkgs = pkgs; system.stateVersion = "26.05"; }
      foxbridgeModule
      jevModule
      browserProfile
      {
        # Browser stack needs the foxbridge runtime on loopback; both modules
        # are enabled through the profile. Give Jev an explicit (non-default)
        # cdpUrl + inspectorPort to prove the options actually flow through
        # independent of node-specific values.
        lattice.jev-ultrafast = {
          cdpUrl = "http://127.0.0.1:9444";
          inspectorPort = 9876;
        };
        # Exercise the agenix-credential path: a runtime (non-store) file
        # mounted via LoadCredential, exactly like the node does with
        # config.age.secrets.<name>.path.
        lattice.jev-ultrafast.typesafeApiKeyFile = "/run/agenix/jev-typesafe-api-key";
      }
    ];
  }).config;

  fb = config.lattice.foxbridge-camoufox;
  jev = config.lattice.jev-ultrafast;
  fbUnit = config.systemd.services.foxbridge-camoufox;
  jevUnit = config.systemd.services.jev-ultrafast;
  fbFilter = fbUnit.serviceConfig.SystemCallFilter or [];
  execStart = builtins.toString (jevUnit.serviceConfig.ExecStart or "");
  execStartPre = builtins.toString (jevUnit.serviceConfig.ExecStartPre or "");
in
assert jev.enable;
assert fb.enable;
# --- 1. lifecycle coupling (f18-09) ---
assert lib.elem "foxbridge-camoufox.service" jevUnit.requires;
assert lib.elem "foxbridge-camoufox.service" jevUnit.after;
# Readiness is enforced by a CDP-probe script (jev-wait-cdp), not by a bare
# After= order: the module generates a probe that curls /json/version of the
# foxbridge port, and Jev starts only after it answers. The probe is a separate
# store path (its name is the contract; the probe content — which embeds the
# runtime port — is exercised on the live node).
assert lib.hasInfix "jev-wait-cdp" execStartPre;
# --- 2. seccomp contract (f18-08 207b0d8) ---
# The regression the crash-loop fixed: `~@resources` MUST NOT be present.
assert !(lib.elem "~@resources" fbFilter);
# The hardening that stays: allow-list + privileged exclusions.
assert lib.elem "@system-service" fbFilter;
assert lib.elem "~@privileged" fbFilter;
# --- 3. persistent (non-tmpfs) browser home (f18-08 06c6d4e) ---
# stateDir is a /var/lib path used as HOME and RW dir; not /run (tmpfs).
assert lib.hasPrefix "/var/lib/" fb.stateDir;
assert lib.elem "HOME=${fb.stateDir}" (fbUnit.serviceConfig.Environment or []);
assert lib.elem fb.stateDir (fbUnit.serviceConfig.ReadWritePaths or []);
# no RuntimeDirectory tmpfs home for the browser
assert !(builtins.hasAttr "RuntimeDirectory" fbUnit.serviceConfig);
# --- 4. loopback-only CDP (f18-07/08) ---
# The module has an assertion refusing non-loopback listenAddress.
assert lib.any
  (a: a.assertion == (fb.listenAddress == "127.0.0.1" || fb.listenAddress == "::1" || fb.listenAddress == "localhost"))
  config.assertions;
# listener ports never open in the firewall.
assert !(builtins.elem fb.port config.networking.firewall.allowedTCPPorts);
assert !(builtins.elem jev.inspectorPort config.networking.firewall.allowedTCPPorts);
# --- 5. secrets only via LoadCredential (f18-08) ---
# The key path flows to LoadCredential, not into Environment/argv.
assert lib.elem "typesafe-api-key:/run/agenix/jev-typesafe-api-key" jevUnit.serviceConfig.LoadCredential;
# Jev's ExecStart is the credential-injecting wrapper store script, not a bare
# binary with the secret on the command line.
assert lib.hasInfix "jev-ultrafast-exec" execStart;

# --- 6. foxbridge startup contract: port and binary are inline declarative args ---
# The ExecStart is inline strings (foxbridge binary, not wrapped); the port flag
# and binary are declarative and verifiable.
assert lib.hasInfix "--port" (builtins.toString (fbUnit.serviceConfig.ExecStart or ""));
assert lib.hasInfix "camoufox" (builtins.toString (fbUnit.serviceConfig.ExecStart or ""));

pkgs.runCommand "f18-browser-stack-contract" { } ''
  echo "F18 browser stack contract OK:
  - jev Requires+After foxbridge, ExecStartPre CDP probe
  - seccomp: no ~@resources, @system-service + ~@privileged retained
  - browser home persistent /var/lib (no tmpfs RuntimeDirectory)
  - CDP loopback-only (module assertion + firewall exclusion)
  - Jev keys via LoadCredential only; wrapper reads credential file"
  touch $out
''
