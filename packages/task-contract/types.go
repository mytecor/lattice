package taskcontract

import (
	"fmt"
)

// ResultStatus represents the terminal state of a task execution.
type ResultStatus string

const (
	StatusCompleted ResultStatus = "completed"
	StatusFailed    ResultStatus = "failed"
	StatusBlocked   ResultStatus = "blocked"
)

// Valid terminal states.
var validTerminalStatuses = map[ResultStatus]struct{}{
	StatusCompleted: {},
	StatusFailed:    {},
	StatusBlocked:   {},
}

// IsTerminal returns true if the status is one of the valid terminal states.
func (s ResultStatus) IsTerminal() bool {
	_, ok := validTerminalStatuses[s]
	return ok
}

// Validate checks whether the status is a valid terminal status.
func (s ResultStatus) Validate() error {
	if !s.IsTerminal() {
		return fmt.Errorf("invalid terminal status %q (must be %s, %s, or %s)",
			s, StatusCompleted, StatusFailed, StatusBlocked)
	}
	return nil
}

// TaskResult defines the terminal result of a task execution.
type TaskResult struct {
	Status           ResultStatus `yaml:"status" json:"status"`
	Revision         string       `yaml:"revision,omitempty" json:"revision,omitempty"`
	ResultText       string       `yaml:"-" json:"result_text,omitempty"`
	VerificationText string       `yaml:"-" json:"verification_text,omitempty"`
}

// IsTerminal checks if the task result represents a completed terminal state.
func (r *TaskResult) IsTerminal() bool {
	return r != nil && r.Status.IsTerminal()
}

// SourceSpec identifies the target codebase repository and revision for the task.
type SourceSpec struct {
	Repo     string `yaml:"repo" json:"repo"`
	Revision string `yaml:"revision" json:"revision"`
}

// RuntimeSpec specifies the container image and execution policies for the task.
type RuntimeSpec struct {
	Image         string `yaml:"image" json:"image"`
	MaxAttempts   int    `yaml:"max_attempts,omitempty" json:"max_attempts,omitempty"`
	MaxIterations int    `yaml:"max_iterations,omitempty" json:"max_iterations,omitempty"`
	Timeout       string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// TaskSpec is the immutable specification of a specific task revision,
// constructed from a task repository, path, and Git commit.
type TaskSpec struct {
	// Git revision identity
	TaskRepo string `json:"task_repo"`
	Path     string `json:"path"`
	Commit   string `json:"commit"`

	// Declared task contract
	ID      string      `json:"id"`
	Source  SourceSpec  `json:"source"`
	Runtime RuntimeSpec `json:"runtime"`
	Checks  []string    `json:"checks,omitempty"`

	// Textual sections from Markdown
	Title              string `json:"title,omitempty"`
	Context            string `json:"context,omitempty"`
	Requirements       string `json:"requirements,omitempty"`
	AcceptanceCriteria string `json:"acceptance_criteria,omitempty"`

	// Terminal result (if recorded in this revision)
	Result *TaskResult `json:"result,omitempty"`
}

// IsActionable returns true if the task revision has not reached a terminal result yet.
// According to F11 / TASK_EXECUTION.md, a revision is actionable until a terminal result appears.
func (s *TaskSpec) IsActionable() bool {
	return s.Result == nil || !s.Result.IsTerminal()
}

// RevisionKey returns a canonical immutable string identifier for this task revision.
func (s *TaskSpec) RevisionKey() string {
	return fmt.Sprintf("%s:%s@%s", s.TaskRepo, s.Path, s.Commit)
}

// DefaultSubjectAgentRun is the default Meshbus subject for dispatching tasks to agentd.
const DefaultSubjectAgentRun = "agent.run"

// RunTaskRequest represents a meshbus request to agentd to execute a TaskSpec.
type RunTaskRequest struct {
	ID      string   `json:"id"`
	Spec    TaskSpec `json:"spec"`
	ReplyTo string   `json:"reply_to"`
}

// RunTaskResponse represents the meshbus reply from agentd.
type RunTaskResponse struct {
	ID     string      `json:"id"`
	Result *TaskResult `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

