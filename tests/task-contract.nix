{ pkgs, taskContract }:

pkgs.runCommand "task-contract-smoke" {
  nativeBuildInputs = [ taskContract ];
} ''
  cat <<'EOF' > valid-actionable.md
---
id: task-smoke-01
source:
  repo: git://example/repo.git
  revision: 123456
runtime:
  image: test:latest
checks:
  - make test
---
# Smoke Task
## Context
Smoke test
EOF

  cat <<'EOF' > valid-terminal.md
---
id: task-smoke-02
source:
  repo: git://example/repo.git
  revision: 123456
runtime:
  image: test:latest
result:
  status: completed
  revision: deadbeef
---
# Smoke Terminal Task
## Result
Done
EOF

  cat <<'EOF' > invalid-runtime-state.md
---
id: task-smoke-03
source:
  repo: git://example/repo.git
  revision: 123456
runtime:
  image: test:latest
state: running
---
# Invalid Task
EOF

  # 1. Actionable task passes validation
  task-validate valid-actionable.md

  # 2. Terminal task passes validation
  task-validate valid-terminal.md

  # 3. Task with forbidden runtime state must be rejected with non-zero exit code
  if task-validate invalid-runtime-state.md 2>/dev/null; then
    echo "ERROR: expected task-validate to fail on invalid-runtime-state.md, but it succeeded" >&2
    exit 1
  fi

  touch "$out"
''
