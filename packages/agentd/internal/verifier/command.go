package verifier

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	taskcontract "github.com/mytecor/lattice/task-contract"
)

// CommandVerifier executes declarative shell commands from spec.Checks inside workDir.
type CommandVerifier struct{}

// NewCommandVerifier creates a new CommandVerifier.
func NewCommandVerifier() *CommandVerifier {
	return &CommandVerifier{}
}

// Verify executes spec.Checks in the provided workDir and collects outputs.
func (v *CommandVerifier) Verify(ctx context.Context, spec *taskcontract.TaskSpec, workDir string) (*Result, error) {
	if spec == nil {
		return nil, fmt.Errorf("cannot verify nil TaskSpec")
	}

	if len(spec.Checks) == 0 {
		return &Result{
			Passed:  true,
			Summary: "No verification checks declared in TaskSpec",
		}, nil
	}

	var outputBuf bytes.Buffer
	var failedChecks []string

	for _, checkCmd := range spec.Checks {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		cmd := exec.CommandContext(ctx, "sh", "-c", checkCmd)
		if workDir != "" {
			cmd.Dir = workDir
		}
		var cmdOut bytes.Buffer
		cmd.Stdout = &cmdOut
		cmd.Stderr = &cmdOut

		err := cmd.Run()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		outStr := cmdOut.String()
		if outStr != "" {
			outputBuf.WriteString(fmt.Sprintf("=== Check: %s ===\n%s\n", checkCmd, outStr))
		}

		if err != nil {
			failedChecks = append(failedChecks, checkCmd)
			outputBuf.WriteString(fmt.Sprintf("=== Check %q failed: %v ===\n", checkCmd, err))
		}
	}

	if len(failedChecks) > 0 {
		return &Result{
			Passed:       false,
			Summary:      fmt.Sprintf("%d of %d verification checks failed: %s", len(failedChecks), len(spec.Checks), strings.Join(failedChecks, ", ")),
			Output:       outputBuf.String(),
			FailedChecks: failedChecks,
		}, nil
	}

	return &Result{
		Passed:  true,
		Summary: fmt.Sprintf("All %d verification checks passed", len(spec.Checks)),
		Output:  outputBuf.String(),
	}, nil
}
