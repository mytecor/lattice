package taskcontract_test

import (
	"reflect"
	"strings"
	"testing"

	taskcontract "github.com/mytecor/lattice/task-contract"
)

const sampleActionableTask = `---
id: task-demo-01

source:
  repo: git://github.com/example/sample-repo.git
  revision: 8f3a1b4c9e

runtime:
  image: ghcr.io/example/agent-runner:latest
  max_attempts: 3
  max_iterations: 10
  timeout: 15m

checks:
  - go test ./...
  - nix flake check
---

# Demo Task Title

## Context

Background context explaining why this task is needed.

## Requirements

1. Requirement one.
2. Requirement two.

## Acceptance criteria

All tests pass and code is verified.
`

const sampleCompletedTask = `---
id: task-demo-02

source:
  repo: git://github.com/example/sample-repo.git
  revision: 8f3a1b4c9e

runtime:
  image: ghcr.io/example/agent-runner:latest

result:
  status: completed
  revision: 1122334455667788
---

# Finished Task

## Context

Some background context.

## Requirements

Do something.

## Acceptance criteria

Done.

## Result

Successfully completed all requirements.

## Verification

Ran all checks, output clean.
`

func TestParseValidActionableTask(t *testing.T) {
	repo := "repo-tasks"
	path := "tasks/demo-01.md"
	commit := "abc123456789"

	spec, err := taskcontract.ParseTask(repo, path, commit, []byte(sampleActionableTask))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if spec.TaskRepo != repo || spec.Path != path || spec.Commit != commit {
		t.Errorf("metadata mismatch: repo=%q, path=%q, commit=%q", spec.TaskRepo, spec.Path, spec.Commit)
	}
	if spec.ID != "task-demo-01" {
		t.Errorf("expected ID 'task-demo-01', got %q", spec.ID)
	}
	if spec.Source.Repo != "git://github.com/example/sample-repo.git" || spec.Source.Revision != "8f3a1b4c9e" {
		t.Errorf("unexpected source: %+v", spec.Source)
	}
	if spec.Runtime.Image != "ghcr.io/example/agent-runner:latest" || spec.Runtime.MaxAttempts != 3 || spec.Runtime.MaxIterations != 10 || spec.Runtime.Timeout != "15m" {
		t.Errorf("unexpected runtime: %+v", spec.Runtime)
	}
	if len(spec.Checks) != 2 || spec.Checks[0] != "go test ./..." || spec.Checks[1] != "nix flake check" {
		t.Errorf("unexpected checks: %v", spec.Checks)
	}
	if spec.Title != "Demo Task Title" {
		t.Errorf("unexpected title: %q", spec.Title)
	}
	if !strings.Contains(spec.Context, "Background context") {
		t.Errorf("unexpected context: %q", spec.Context)
	}
	if !strings.Contains(spec.Requirements, "Requirement one") {
		t.Errorf("unexpected requirements: %q", spec.Requirements)
	}
	if !strings.Contains(spec.AcceptanceCriteria, "All tests pass") {
		t.Errorf("unexpected acceptance criteria: %q", spec.AcceptanceCriteria)
	}

	// Invariant: without result, task is actionable
	if !spec.IsActionable() {
		t.Errorf("expected task to be actionable, but IsActionable() was false")
	}
	if spec.Result != nil {
		t.Errorf("expected nil Result for actionable task, got %+v", spec.Result)
	}
}

func TestParseValidTerminalTask_Completed(t *testing.T) {
	spec, err := taskcontract.ParseTask("repo", "tasks/demo-02.md", "commit-sha", []byte(sampleCompletedTask))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	// Invariant: with terminal result, task is not actionable
	if spec.IsActionable() {
		t.Errorf("expected task to NOT be actionable, but IsActionable() was true")
	}
	if spec.Result == nil {
		t.Fatalf("expected non-nil Result")
	}
	if spec.Result.Status != taskcontract.StatusCompleted {
		t.Errorf("expected status 'completed', got %q", spec.Result.Status)
	}
	if spec.Result.Revision != "1122334455667788" {
		t.Errorf("expected revision '1122334455667788', got %q", spec.Result.Revision)
	}
	if !strings.Contains(spec.Result.ResultText, "Successfully completed") {
		t.Errorf("unexpected result text: %q", spec.Result.ResultText)
	}
	if !strings.Contains(spec.Result.VerificationText, "Ran all checks") {
		t.Errorf("unexpected verification text: %q", spec.Result.VerificationText)
	}
}

func TestTerminalStatuses(t *testing.T) {
	for _, status := range []taskcontract.ResultStatus{
		taskcontract.StatusCompleted,
		taskcontract.StatusFailed,
		taskcontract.StatusBlocked,
	} {
		t.Run(string(status), func(t *testing.T) {
			if !status.IsTerminal() {
				t.Errorf("expected status %s to be terminal", status)
			}
			content := strings.Replace(sampleCompletedTask, "status: completed", "status: "+string(status), 1)
			spec, err := taskcontract.ParseTaskDocument([]byte(content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if spec.IsActionable() {
				t.Errorf("expected status %s to make task not actionable", status)
			}
			if spec.Result.Status != status {
				t.Errorf("expected status %s, got %s", status, spec.Result.Status)
			}
		})
	}
}

func TestDeterministicTaskSpec(t *testing.T) {
	repo := "repo-a"
	path := "task.md"
	commit := "commitaabbcc"
	content := []byte(sampleActionableTask)

	spec1, err1 := taskcontract.ParseTask(repo, path, commit, content)
	spec2, err2 := taskcontract.ParseTask(repo, path, commit, content)

	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected error during deterministic parse test: %v, %v", err1, err2)
	}
	if !reflect.DeepEqual(spec1, spec2) {
		t.Errorf("parser is not deterministic: spec1 != spec2")
	}
	if spec1.RevisionKey() != spec2.RevisionKey() {
		t.Errorf("expected matching revision keys: %q vs %q", spec1.RevisionKey(), spec2.RevisionKey())
	}
}

func TestStrictRejectionOfForbiddenRuntimeFields(t *testing.T) {
	forbiddenCases := []struct {
		name    string
		snippet string
	}{
		{"state_running", "state: running"},
		{"state_in_progress", "state: in_progress"},
		{"worker", "worker: worker-node-01"},
		{"worker_id", "worker_id: w-123"},
		{"execution", "execution: exec-xyz"},
		{"execution_id", "execution_id: exec-xyz"},
		{"attempt", "attempt: 2"},
		{"current_attempt", "current_attempt: 1"},
		{"lease", "lease: acquired"},
		{"lease_id", "lease_id: lease-abc"},
		{"heartbeat", "heartbeat: 123456789"},
		{"session", "session: acp-session-1"},
		{"session_id", "session_id: acp-session-1"},
		{"tunnel", "tunnel: localhost:9000"},
		{"unknown_field", "custom_property: invalid"},
	}

	baseTemplate := `---
id: task-test
source:
  repo: repo
  revision: ref
runtime:
  image: test-image
%s
---
# Title
`

	for _, tc := range forbiddenCases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(strings.Replace(baseTemplate, "%s", tc.snippet, 1))
			_, err := taskcontract.ParseTaskDocument(raw)
			if err == nil {
				t.Fatalf("expected error for forbidden runtime field %q, got nil", tc.snippet)
			}
			if !strings.Contains(err.Error(), "forbidden") && !strings.Contains(err.Error(), "unknown") {
				t.Errorf("expected error message to mention forbidden or unknown, got: %v", err)
			}
		})
	}
}

func TestStrictRejectionOfRuntimeStatusInResult(t *testing.T) {
	invalidStatuses := []string{
		"running",
		"in_progress",
		"in-progress",
		"pending",
		"scheduled",
		"queued",
		"unknown",
	}

	baseTemplate := `---
id: task-test
source:
  repo: repo
  revision: ref
runtime:
  image: test-image
result:
  status: %s
---
# Title
`

	for _, status := range invalidStatuses {
		t.Run(status, func(t *testing.T) {
			raw := []byte(strings.Replace(baseTemplate, "%s", status, 1))
			_, err := taskcontract.ParseTaskDocument(raw)
			if err == nil {
				t.Fatalf("expected error for non-terminal result status %q, got nil", status)
			}
		})
	}
}

func TestValidationErrorsOnMissingRequiredFields(t *testing.T) {
	testCases := []struct {
		name    string
		content string
	}{
		{
			"missing_id",
			`---
source: { repo: r, revision: c }
runtime: { image: img }
---
# Title`,
		},
		{
			"missing_source",
			`---
id: task-1
runtime: { image: img }
---
# Title`,
		},
		{
			"missing_source_repo",
			`---
id: task-1
source: { revision: c }
runtime: { image: img }
---
# Title`,
		},
		{
			"missing_source_revision",
			`---
id: task-1
source: { repo: r }
runtime: { image: img }
---
# Title`,
		},
		{
			"missing_runtime",
			`---
id: task-1
source: { repo: r, revision: c }
---
# Title`,
		},
		{
			"missing_runtime_image",
			`---
id: task-1
source: { repo: r, revision: c }
runtime: { max_attempts: 1 }
---
# Title`,
		},
		{
			"negative_max_attempts",
			`---
id: task-1
source: { repo: r, revision: c }
runtime: { image: img, max_attempts: -1 }
---
# Title`,
		},
		{
			"missing_frontmatter",
			`# Just markdown without front matter`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := taskcontract.ParseTaskDocument([]byte(tc.content))
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestFormatTaskWithResult(t *testing.T) {
	original := []byte(sampleActionableTask)

	res := &taskcontract.TaskResult{
		Status:           taskcontract.StatusCompleted,
		Revision:         "result-commit-9999",
		ResultText:       "Successfully completed feature implementation.",
		VerificationText: "go test passed with 100% coverage.",
	}

	updated, err := taskcontract.FormatTaskWithResult(original, res)
	if err != nil {
		t.Fatalf("unexpected format error: %v", err)
	}

	// Re-parse the updated content
	spec, err := taskcontract.ParseTask("repo", "tasks/demo-01.md", "commit-new", updated)
	if err != nil {
		t.Fatalf("failed to parse updated task: %v", err)
	}

	if spec.IsActionable() {
		t.Errorf("expected updated task to be non-actionable (terminal)")
	}
	if spec.Result == nil {
		t.Fatalf("expected result to be present")
	}
	if spec.Result.Status != taskcontract.StatusCompleted {
		t.Errorf("expected status 'completed', got %q", spec.Result.Status)
	}
	if spec.Result.Revision != "result-commit-9999" {
		t.Errorf("expected revision 'result-commit-9999', got %q", spec.Result.Revision)
	}
	if !strings.Contains(spec.Result.ResultText, "Successfully completed feature implementation") {
		t.Errorf("unexpected ResultText: %q", spec.Result.ResultText)
	}
	if !strings.Contains(spec.Result.VerificationText, "go test passed") {
		t.Errorf("unexpected VerificationText: %q", spec.Result.VerificationText)
	}
}
