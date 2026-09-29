package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"moofetch/internal/domain"
)

type customFatalAuthError struct {
	fatal bool
	msg   string
}

func (e *customFatalAuthError) Error() string {
	return e.msg
}

func (e *customFatalAuthError) IsFatalAuth() bool {
	return e.fatal
}

func TestIsFatalAuth(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "ErrAuthenticationFailed sentinel error",
			err:      domain.ErrAuthenticationFailed,
			expected: true,
		},
		{
			name:     "wrapped ErrAuthenticationFailed",
			err:      fmt.Errorf("moodle plugin error: %w", domain.ErrAuthenticationFailed),
			expected: true,
		},
		{
			name:     "custom error implementing FatalAuthError returning true",
			err:      &customFatalAuthError{fatal: true, msg: "custom auth rejection"},
			expected: true,
		},
		{
			name:     "custom error implementing FatalAuthError returning false without auth keyword",
			err:      &customFatalAuthError{fatal: false, msg: "temporary connection glitch"},
			expected: false,
		},
		{
			name:     "arbitrary error with authentication failed text lowercase",
			err:      errors.New("remote server reported: authentication failed"),
			expected: true,
		},
		{
			name:     "arbitrary error with authentication failed text mixed case",
			err:      errors.New("HTTP 401: Authentication Failed"),
			expected: true,
		},
		{
			name:     "arbitrary unrelated error",
			err:      errors.New("dial tcp: i/o timeout"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := domain.IsFatalAuth(tt.err)
			if result != tt.expected {
				t.Errorf("IsFatalAuth(%v) = %v; want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestDomainStructsAndConstants(t *testing.T) {
	task := domain.Task{
		ID:        1,
		URL:       "https://example.com/file.zip",
		Cookie:    "session=abc",
		OutputDir: "/tmp",
	}
	if task.ID != 1 || task.URL != "https://example.com/file.zip" {
		t.Fatalf("unexpected task values: %+v", task)
	}

	result := domain.Result{
		TaskID:     1,
		URL:        task.URL,
		Filename:   "file.zip",
		BytesRead:  100,
		TotalBytes: 100,
		Err:        nil,
	}
	if result.Filename != "file.zip" || result.BytesRead != 100 {
		t.Fatalf("unexpected result values: %+v", result)
	}

	update := domain.ProgressUpdate{
		TaskID:     1,
		URL:        task.URL,
		Filename:   "file.zip",
		BytesRead:  50,
		TotalBytes: 100,
	}
	var receivedUpdate domain.ProgressUpdate
	var progressFn domain.ProgressFunc = func(u domain.ProgressUpdate) {
		receivedUpdate = u
	}
	progressFn(update)
	if receivedUpdate.BytesRead != 50 {
		t.Fatalf("progress function failed, expected 50 bytes read, got %d", receivedUpdate.BytesRead)
	}

	event := domain.Event{
		Type:     domain.EventTaskStarted,
		TaskID:   1,
		URL:      task.URL,
		Filename: "file.zip",
	}
	var receivedEvent domain.Event
	var handler domain.EventHandler = func(e domain.Event) {
		receivedEvent = e
	}
	handler(event)
	if receivedEvent.Type != domain.EventTaskStarted {
		t.Fatalf("event handler failed, expected EventTaskStarted, got %v", receivedEvent.Type)
	}

	eventTypes := []domain.EventType{
		domain.EventTaskStarted,
		domain.EventTaskProgress,
		domain.EventTaskCompleted,
		domain.EventTaskFailed,
	}
	for i, et := range eventTypes {
		if int(et) != i {
			t.Errorf("expected EventType %d to equal %d", et, i)
		}
	}
}
