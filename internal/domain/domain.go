// Package domain defines pure domain models, event primitives, and domain-level errors
// with zero internal dependencies across MooFetch.
package domain

import (
	"errors"
	"strings"
)

var (
	// ErrAuthenticationFailed indicates invalid or expired credentials/session that aborts the batch.
	ErrAuthenticationFailed = errors.New("authentication failed")
)

// FatalAuthError is an interface implemented by errors that represent unrecoverable auth failures.
type FatalAuthError interface {
	IsFatalAuth() bool
}

// IsFatalAuth checks if an error represents an unrecoverable authentication failure.
func IsFatalAuth(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAuthenticationFailed) {
		return true
	}
	var fa FatalAuthError
	if errors.As(err, &fa) && fa.IsFatalAuth() {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "authentication failed")
}

// Task represents an individual download unit.
type Task struct {
	ID        int
	URL       string
	Cookie    string
	OutputDir string
}

// Result captures the final status and outcome of a download task.
type Result struct {
	TaskID     int
	URL        string
	Filename   string
	BytesRead  int64
	TotalBytes int64
	Err        error
}

// ProgressUpdate contains real-time stream information for a running task.
type ProgressUpdate struct {
	TaskID     int
	URL        string
	Filename   string
	BytesRead  int64
	TotalBytes int64
}

// ProgressFunc is a callback invoked during payload transfer.
type ProgressFunc func(update ProgressUpdate)

// EventType categorizes dispatcher event notifications.
type EventType int

const (
	// EventTaskStarted indicates a task was picked up by an active worker.
	EventTaskStarted EventType = iota
	// EventTaskProgress indicates byte transfer progress for a task.
	EventTaskProgress
	// EventTaskCompleted indicates a task finished successfully.
	EventTaskCompleted
	// EventTaskFailed indicates a task terminated with an error.
	EventTaskFailed
)

// Event represents an atomic status transition sent to observers.
type Event struct {
	Type     EventType
	TaskID   int
	URL      string
	Filename string
	Bytes    int64
	Total    int64
	Err      error
}

// EventHandler receives real-time download events from the dispatcher.
type EventHandler func(event Event)
