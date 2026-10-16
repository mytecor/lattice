package taskcontract

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// FormatTask serializes a TaskSpec back into a Markdown document with YAML front matter.
func FormatTask(spec *TaskSpec) ([]byte, error) {
	if spec == nil {
		return nil, errors.New("cannot format nil TaskSpec")
	}

	fm := rawFrontMatter{
		ID:      spec.ID,
		Source:  &spec.Source,
		Runtime: &spec.Runtime,
		Checks:  spec.Checks,
		Result:  spec.Result,
	}

	if err := validateFrontMatterData(&fm); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")

	fmBytes, err := yaml.Marshal(&fm)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal front matter: %w", err)
	}
	buf.Write(fmBytes)
	buf.WriteString("---\n\n")

	if spec.Title != "" {
		buf.WriteString(fmt.Sprintf("# %s\n\n", spec.Title))
	}

	if spec.Context != "" {
		buf.WriteString("## Context\n\n")
		buf.WriteString(spec.Context)
		buf.WriteString("\n\n")
	}

	if spec.Requirements != "" {
		buf.WriteString("## Requirements\n\n")
		buf.WriteString(spec.Requirements)
		buf.WriteString("\n\n")
	}

	if spec.AcceptanceCriteria != "" {
		buf.WriteString("## Acceptance criteria\n\n")
		buf.WriteString(spec.AcceptanceCriteria)
		buf.WriteString("\n\n")
	}

	if spec.Result != nil {
		if spec.Result.ResultText != "" {
			buf.WriteString("## Result\n\n")
			buf.WriteString(spec.Result.ResultText)
			buf.WriteString("\n\n")
		}
		if spec.Result.VerificationText != "" {
			buf.WriteString("## Verification\n\n")
			buf.WriteString(spec.Result.VerificationText)
			buf.WriteString("\n\n")
		}
	}

	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// FormatTaskWithResult updates an existing task document with a terminal TaskResult.
// It validates that the result is terminal, injects the result block into front matter,
// and appends ## Result and ## Verification sections to the Markdown body.
func FormatTaskWithResult(originalContent []byte, result *TaskResult) ([]byte, error) {
	if result == nil {
		return nil, errors.New("cannot format nil TaskResult")
	}
	if err := result.Status.Validate(); err != nil {
		return nil, err
	}

	// Validate that the original document is valid
	spec, err := ParseTaskDocument(originalContent)
	if err != nil {
		return nil, fmt.Errorf("cannot format result onto invalid task document: %w", err)
	}

	spec.Result = result
	formatted, err := FormatTask(spec)
	if err != nil {
		return nil, err
	}
	// Append final newline for Git file conventions
	return append(formatted, '\n'), nil
}
