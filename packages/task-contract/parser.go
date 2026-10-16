package taskcontract

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Allowed top-level front matter keys.
var allowedTopLevelKeys = map[string]struct{}{
	"id":      {},
	"source":  {},
	"runtime": {},
	"checks":  {},
	"result":  {},
}

// Forbidden runtime keys at any level in front matter.
var forbiddenRuntimeKeys = map[string]struct{}{
	"state":           {},
	"worker":          {},
	"worker_id":       {},
	"execution":       {},
	"execution_id":    {},
	"attempt":         {},
	"attempts":        {},
	"current_attempt": {},
	"lease":           {},
	"lease_id":        {},
	"heartbeat":       {},
	"session":         {},
	"session_id":      {},
	"acp_session":     {},
	"tunnel":          {},
}

// rawFrontMatter is used for unmarshaling the front matter YAML.
type rawFrontMatter struct {
	ID      string       `yaml:"id"`
	Source  *SourceSpec  `yaml:"source"`
	Runtime *RuntimeSpec `yaml:"runtime"`
	Checks  []string     `yaml:"checks"`
	Result  *TaskResult  `yaml:"result"`
}

// ParseTask parses a task Markdown document for a specific Git revision
// identified by taskRepo, path, and commit SHA.
func ParseTask(taskRepo, path, commit string, content []byte) (*TaskSpec, error) {
	if strings.TrimSpace(taskRepo) == "" {
		return nil, errors.New("task repository identifier must not be empty")
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("task path must not be empty")
	}
	if strings.TrimSpace(commit) == "" {
		return nil, errors.New("task commit SHA must not be empty")
	}

	spec, err := ParseTaskDocument(content)
	if err != nil {
		return nil, err
	}

	spec.TaskRepo = taskRepo
	spec.Path = path
	spec.Commit = commit
	return spec, nil
}

// ParseTaskDocument parses a task Markdown document without requiring Git revision metadata.
func ParseTaskDocument(content []byte) (*TaskSpec, error) {
	fmBytes, mdBytes, err := splitFrontMatter(content)
	if err != nil {
		return nil, err
	}

	// First pass: validate YAML node structure for forbidden runtime fields and unknown keys.
	var node yaml.Node
	if err := yaml.Unmarshal(fmBytes, &node); err != nil {
		return nil, fmt.Errorf("invalid YAML front matter: %w", err)
	}

	if err := validateFrontMatterNode(&node); err != nil {
		return nil, err
	}

	// Second pass: decode into typed front matter structure.
	var fm rawFrontMatter
	if err := yaml.Unmarshal(fmBytes, &fm); err != nil {
		return nil, fmt.Errorf("failed to decode front matter: %w", err)
	}

	if err := validateFrontMatterData(&fm); err != nil {
		return nil, err
	}

	spec := &TaskSpec{
		ID:      strings.TrimSpace(fm.ID),
		Source:  *fm.Source,
		Runtime: *fm.Runtime,
		Checks:  cleanChecks(fm.Checks),
		Result:  fm.Result,
	}

	// Parse Markdown sections
	parseMarkdownSections(mdBytes, spec)

	return spec, nil
}

// splitFrontMatter separates YAML front matter from Markdown body.
func splitFrontMatter(content []byte) ([]byte, []byte, error) {
	// Trim UTF-8 BOM if present
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))

	trimmed := bytes.TrimLeft(content, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return nil, nil, errors.New("missing front matter opening delimiter '---'")
	}

	// Rest after opening '---'
	rest := trimmed[3:]
	// Must have newline or space then newline after opening ---
	nlIdx := bytes.IndexByte(rest, '\n')
	if nlIdx == -1 {
		return nil, nil, errors.New("unexpected EOF after front matter opening delimiter")
	}
	rest = rest[nlIdx+1:]

	// Find closing delimiter: either \n---\n or \n...\n or end of file
	lines := bytes.Split(rest, []byte("\n"))
	var fmEndIdx = -1
	var lineOffset = 0

	for _, line := range lines {
		trimmedLine := bytes.TrimSpace(line)
		if bytes.Equal(trimmedLine, []byte("---")) || bytes.Equal(trimmedLine, []byte("...")) {
			fmEndIdx = lineOffset
			break
		}
		lineOffset += len(line) + 1 // +1 for the newline
	}

	if fmEndIdx == -1 {
		return nil, nil, errors.New("missing front matter closing delimiter '---'")
	}

	fmLines := rest[:fmEndIdx]
	mdLines := rest[fmEndIdx:]
	// Skip the closing delimiter line in mdLines
	if closeNl := bytes.IndexByte(mdLines, '\n'); closeNl != -1 {
		mdLines = mdLines[closeNl+1:]
	} else {
		mdLines = nil
	}

	return fmLines, mdLines, nil
}

// validateFrontMatterNode recursively inspects YAML nodes for forbidden runtime keys and unknown properties.
func validateFrontMatterNode(node *yaml.Node) error {
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return errors.New("front matter cannot be empty")
		}
		return validateFrontMatterNode(node.Content[0])
	}

	if node.Kind != yaml.MappingNode {
		return errors.New("front matter root must be a YAML mapping")
	}

	// Validate top-level keys
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		key := strings.ToLower(strings.TrimSpace(keyNode.Value))

		if isForbiddenRuntimeKey(key) {
			return fmt.Errorf("forbidden runtime field %q found in task front matter (line %d)", keyNode.Value, keyNode.Line)
		}

		if _, ok := allowedTopLevelKeys[key]; !ok {
			return fmt.Errorf("unknown or forbidden property %q in task front matter (line %d)", keyNode.Value, keyNode.Line)
		}

		// Recursively check mapping children
		if err := checkForbiddenKeysRecursive(valNode); err != nil {
			return err
		}
	}

	return nil
}

func checkForbiddenKeysRecursive(node *yaml.Node) error {
	if node == nil {
		return nil
	}

	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valNode := node.Content[i+1]
			key := strings.ToLower(strings.TrimSpace(keyNode.Value))

			if isForbiddenRuntimeKey(key) {
				return fmt.Errorf("forbidden runtime field %q found in task front matter (line %d)", keyNode.Value, keyNode.Line)
			}
			if err := checkForbiddenKeysRecursive(valNode); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if err := checkForbiddenKeysRecursive(item); err != nil {
				return err
			}
		}
	}

	return nil
}

func isForbiddenRuntimeKey(key string) bool {
	if _, ok := forbiddenRuntimeKeys[key]; ok {
		return true
	}
	// Also check prefix/suffix patterns like worker_* or *_attempt
	if strings.HasPrefix(key, "worker_") || strings.HasSuffix(key, "_worker") {
		return true
	}
	if strings.HasPrefix(key, "execution_") || strings.HasSuffix(key, "_execution") {
		return true
	}
	if (strings.HasSuffix(key, "_attempt") || strings.HasPrefix(key, "attempt_")) && key != "max_attempts" {
		return true
	}
	return false
}

// validateFrontMatterData validates typed front matter fields and values.
func validateFrontMatterData(fm *rawFrontMatter) error {
	if strings.TrimSpace(fm.ID) == "" {
		return errors.New("task 'id' is required and cannot be empty")
	}

	if fm.Source == nil {
		return errors.New("task 'source' section is required")
	}
	if strings.TrimSpace(fm.Source.Repo) == "" {
		return errors.New("task 'source.repo' is required and cannot be empty")
	}
	if strings.TrimSpace(fm.Source.Revision) == "" {
		return errors.New("task 'source.revision' is required and cannot be empty")
	}

	if fm.Runtime == nil {
		return errors.New("task 'runtime' section is required")
	}
	if strings.TrimSpace(fm.Runtime.Image) == "" {
		return errors.New("task 'runtime.image' is required and cannot be empty")
	}
	if fm.Runtime.MaxAttempts < 0 {
		return errors.New("task 'runtime.max_attempts' cannot be negative")
	}
	if fm.Runtime.MaxIterations < 0 {
		return errors.New("task 'runtime.max_iterations' cannot be negative")
	}

	// Validate Result if present
	if fm.Result != nil {
		status := strings.ToLower(strings.TrimSpace(string(fm.Result.Status)))
		fm.Result.Status = ResultStatus(status)

		// Check for forbidden runtime states in status
		if status == "running" || status == "in_progress" || status == "in-progress" || status == "pending" {
			return fmt.Errorf("forbidden runtime state in result status %q: Git task contract only allows terminal states", status)
		}

		if err := fm.Result.Status.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func cleanChecks(checks []string) []string {
	var cleaned []string
	for _, c := range checks {
		trimmed := strings.TrimSpace(c)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}

// parseMarkdownSections parses headings and body content.
func parseMarkdownSections(md []byte, spec *TaskSpec) {
	if len(md) == 0 {
		return
	}

	lines := strings.Split(string(md), "\n")
	var currentSection string
	var sectionLines = make(map[string][]string)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check H1 title
		if strings.HasPrefix(trimmed, "# ") && spec.Title == "" {
			spec.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			continue
		}

		// Check H2 section headers
		if strings.HasPrefix(trimmed, "## ") {
			headerName := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			currentSection = normalizeSectionHeader(headerName)
			continue
		}

		if currentSection != "" {
			sectionLines[currentSection] = append(sectionLines[currentSection], line)
		}
	}

	spec.Context = trimSection(sectionLines["context"])
	spec.Requirements = trimSection(sectionLines["requirements"])
	spec.AcceptanceCriteria = trimSection(sectionLines["acceptance criteria"])

	resultText := trimSection(sectionLines["result"])
	verificationText := trimSection(sectionLines["verification"])

	if spec.Result != nil {
		spec.Result.ResultText = resultText
		spec.Result.VerificationText = verificationText
	}
}

func normalizeSectionHeader(header string) string {
	switch header {
	case "context":
		return "context"
	case "requirements", "requirement":
		return "requirements"
	case "acceptance criteria", "acceptance criterion", "acceptance":
		return "acceptance criteria"
	case "result", "results":
		return "result"
	case "verification", "verifications":
		return "verification"
	default:
		return header
	}
}

func trimSection(lines []string) string {
	text := strings.Join(lines, "\n")
	return strings.TrimSpace(text)
}
