# Tests

Testing policy for the Lattice repository. All tests are reachable from a single
CI entry point:

```bash
nix flake check --print-build-logs
```

The goal is a high signal-to-maintenance ratio: a test should catch a real
regression, not merely diff the current configuration. Before adding or changing
a test, read this file and ask *what real regression does this catch?* If there
is no convincing answer, the test does not earn its place.

## Categories

### Evaluation/build tests

Verify that a NixOS configuration successfully evaluates and builds. They catch
broken option usage, missing imports, malformed profiles and module wiring.

Examples: `checks.x86_64-linux.example`,
`checks.x86_64-linux.mytecor-homelab` in [`default.nix`](./default.nix).

### Module contract tests

Verify the API of a module: its defaults, validation, and expected failures.
These use the `valid input -> evaluation succeeds / invalid input -> module
assertion fails` scheme against an *isolated* configuration, never against the
production node.

Examples: [`rns-network.nix`](./rns-network.nix), [`rns-tcp.nix`](./rns-tcp.nix).

A rule that must hold for **all** users of a module belongs in the module's own
`config.assertions` with a clear diagnostic message, not in a test of a specific
node. Only rules that are genuinely module-level go there; node-specific values
stay out of both.

### Generated artifact tests

Verify the artifacts Nix generates: JSON configs, Caddy configuration, systemd
unit properties, generated command lines, credential/environment wiring. Check
*semantically important properties* of the artifact, not the whole generated
file. Prefer membership over exact equality:

```nix
# good: additional ports are legitimate
assert builtins.elem 80 config.networking.firewall.allowedTCPPorts;

# brittle: adding any port breaks the test
# assert config.networking.firewall.allowedTCPPorts == [ 80 ];
```

Examples: [`app-services.nix`](./app-services.nix),
[`llm-gateway-bifrost.nix`](./llm-gateway-bifrost.nix) (jq over the generated
public config, `caddy adapt --validate`, generated JSON),
[`pi-acp-daemon.nix`](./pi-acp-daemon.nix).

### NixOS integration tests

`pkgs.testers.runNixOSTest` VM tests for behavior that cannot be reliably proven
by evaluation alone: a service actually starts, a file is materialized, a
symlink exists, a runtime directory is writable, an executable is available, an
endpoint responds, several services interact. Do not move these checks back into
plain Nix `assert` blocks.

Examples: [`llm-gateway-service.nix`](./llm-gateway-service.nix),
[`pi-config.nix`](./pi-config.nix),
[`comin-source-sync.nix`](./comin-source-sync.nix).

### Application tests

Go / JS / Rust / other tests live next to the application code itself (for
example `packages/llm-gateway/*_test.go`). Nix tests must not re-test the
application's internal logic; they verify only the integration boundary:

```text
Nix config
    ↓
generated config
    ↓
application
```

i.e. that Nix handed the right data to the application, not that the application
implements some feature correctly.

## What not to test

Do not write a test only because a value is present in the production config.

Bad examples (ordinary configuration changes that must not break tests):

```nix
assert cfg.port == 1234;
assert builtins.length cfg.providers == 6;
assert cfg.defaultModel == "foo";
```

A good test checks a contract or invariant:

```nix
assert !cfg.services.openssh.settings.PasswordAuthentication;
```

if that is the project's security requirement, or:

```nix
assert lib.all validProvider cfg.providers;
```

if that is a rule of the system.

Guiding questions for every test:

1. What real regression does it catch?
2. Would a normal Nix evaluation catch this regression without the test?
3. Is this an invariant, or just the current value of the config?
4. Could the rule be moved into a module `assertion`?
5. Is this logic already tested at the application level?
6. Can a brittle equality be replaced by a property check?

Remove assertions with no convincing answer to question 1.
