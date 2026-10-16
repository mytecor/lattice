package main

// Default subject names on Meshbus.
const (
	DefaultSubjectHead  = "git.head"
	DefaultSubjectWatch = "git.watch"
)

// Watch response statuses.
const (
	StatusChanged  = "changed"
	StatusNoChange = "no_change"
	StatusError    = "error"
)

// HeadRequest represents a request to resolve current HEAD of a local repo/ref.
type HeadRequest struct {
	ID      string `json:"id"`
	Repo    string `json:"repo"`
	Ref     string `json:"ref"`
	ReplyTo string `json:"reply_to"`
}

// HeadResponse represents the reply to a HeadRequest.
type HeadResponse struct {
	ID       string `json:"id"`
	Revision string `json:"revision,omitempty"`
	Error    string `json:"error,omitempty"`
}

// WatchRequest represents a request to watch for changes after after_revision.
type WatchRequest struct {
	ID            string `json:"id"`
	Repo          string `json:"repo"`
	Ref           string `json:"ref"`
	AfterRevision string `json:"after_revision,omitempty"`
	TimeoutMs     int64  `json:"timeout_ms,omitempty"`
	ReplyTo       string `json:"reply_to"`
}

// WatchResponse represents the reply to a WatchRequest.
type WatchResponse struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"` // "changed", "no_change", "error"
	From         string   `json:"from,omitempty"`
	To           string   `json:"to,omitempty"`
	ChangedPaths []string `json:"changed_paths,omitempty"`
	Revision     string   `json:"revision,omitempty"`
	Error        string   `json:"error,omitempty"`
}
