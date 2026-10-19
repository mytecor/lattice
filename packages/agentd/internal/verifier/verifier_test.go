package verifier

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	taskcontract "github.com/mytecor/lattice/task-contract"
)

func TestCommandVerifier_NoChecks(t *testing.T) {
	v := NewCommandVerifier()
	spec := &taskcontract.TaskSpec{
		ID: "task-test-01",
	}

	res, err := v.Verify(context.Background(), spec, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Fatalf("expected passed=true when no checks declared, got %+v", res)
	}
}

func TestCommandVerifier_Success(t *testing.T) {
	v := NewCommandVerifier()
	tempDir := t.TempDir()

	spec := &taskcontract.TaskSpec{
		ID: "task-test-02",
		Checks: []string{
			"echo hello",
			"test -d .",
		},
	}

	res, err := v.Verify(context.Background(), spec, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Fatalf("expected passed=true, got %+v", res)
	}
	if !strings.Contains(res.Output, "hello") {
		t.Errorf("expected output to contain 'hello', got %s", res.Output)
	}
}

func TestCommandVerifier_Failure(t *testing.T) {
	v := NewCommandVerifier()
	tempDir := t.TempDir()

	spec := &taskcontract.TaskSpec{
		ID: "task-test-03",
		Checks: []string{
			"echo starting-check",
			"test -f non-existent-file.txt",
		},
	}

	res, err := v.Verify(context.Background(), spec, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatalf("expected passed=false, got passed=true")
	}
	if len(res.FailedChecks) != 1 || res.FailedChecks[0] != "test -f non-existent-file.txt" {
		t.Errorf("expected failed check 'test -f non-existent-file.txt', got %v", res.FailedChecks)
	}
	if !strings.Contains(res.Output, "starting-check") {
		t.Errorf("expected output to contain stdout of prior checks, got %s", res.Output)
	}
}

func TestCommandVerifier_WorkspaceAware(t *testing.T) {
	v := NewCommandVerifier()
	tempDir := t.TempDir()

	markerFile := filepath.Join(tempDir, "marker.txt")
	if err := os.WriteFile(markerFile, []byte("ready"), 0644); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}

	spec := &taskcontract.TaskSpec{
		ID: "task-test-04",
		Checks: []string{
			"grep ready marker.txt",
		},
	}

	res, err := v.Verify(context.Background(), spec, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Fatalf("expected passed=true in workDir, got %+v", res)
	}
}

func TestCommandVerifier_ContextCancellation(t *testing.T) {
	v := NewCommandVerifier()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	spec := &taskcontract.TaskSpec{
		ID: "task-test-05",
		Checks: []string{
			"sleep 2",
		},
	}

	_, err := v.Verify(ctx, spec, "")
	if err == nil {
		t.Fatal("expected error due to context cancellation, got nil")
	}
}
