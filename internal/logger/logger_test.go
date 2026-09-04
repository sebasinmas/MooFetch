package logger_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"godownloader/internal/logger"
)

func TestLogger_WriteAndRead(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "test_debug.txt")

	l, err := logger.New(logPath)
	if err != nil {
		t.Fatalf("failed creating logger: %v", err)
	}

	l.Printf("System initialized with %d workers", 5)
	l.LogTaskStart(1, "https://campusvirtual.ufro.cl/test.pdf")
	l.LogTaskRedirect(1, "https://campusvirtual.ufro.cl/view.php", "https://campusvirtual.ufro.cl/pluginfile.php", 303)
	l.LogTaskSuccess(1, "test.pdf", 1024)

	if err := l.Close(); err != nil {
		t.Fatalf("failed closing logger: %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed reading log file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "GoDownloader Debug Log") {
		t.Errorf("expected header in log, got: %s", contentStr)
	}
	if !strings.Contains(contentStr, "System initialized with 5 workers") {
		t.Errorf("expected worker log entry")
	}
	if !strings.Contains(contentStr, "task_id=1") {
		t.Errorf("expected task_id=1 in log output")
	}
	if !strings.Contains(contentStr, "status=303") {
		t.Errorf("expected status=303 in log output")
	}
}

func TestLogger_TypedAttributesAndSlogIntegration(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "typed_attrs.txt")

	l, err := logger.New(logPath)
	if err != nil {
		t.Fatalf("failed creating logger: %v", err)
	}

	// DoD verification: logger.Info("download finished", slog.Int("task_id", id), slog.Int64("bytes", n))
	l.Info("download finished", slog.Int("task_id", 42), slog.Int64("bytes", 999999))
	l.Debug("debug message", slog.String("component", "transport"))
	l.Warn("warning event", slog.String("reason", "slow server"))

	if err := l.Close(); err != nil {
		t.Fatalf("failed closing logger: %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed reading log file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, `msg="download finished"`) {
		t.Errorf("expected message in log: %s", contentStr)
	}
	if !strings.Contains(contentStr, "task_id=42") {
		t.Errorf("expected typed task_id attribute")
	}
	if !strings.Contains(contentStr, "bytes=999999") {
		t.Errorf("expected typed bytes attribute")
	}
	if !strings.Contains(contentStr, "component=transport") {
		t.Errorf("expected debug message attribute")
	}
}

func TestLogger_SessionCookieLogValuerRedaction(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "cookie_redaction.txt")

	l, err := logger.New(logPath)
	if err != nil {
		t.Fatalf("failed creating logger: %v", err)
	}

	rawSecret := "supersecretcookieval1234567890"
	cookieVal := logger.SessionCookie("MoodleSession=" + rawSecret)

	// Log using typed SessionCookie (LogValuer)
	l.Info("session established", slog.Any("cookie", cookieVal))

	// Log using raw string attribute key containing cookie
	l.Info("raw cookie header", slog.String("session_cookie", "MoodleSession="+rawSecret))

	if err := l.Close(); err != nil {
		t.Fatalf("failed closing logger: %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed reading log file: %v", err)
	}

	contentStr := string(content)
	// Secret token must NEVER be written in plain text
	if strings.Contains(contentStr, rawSecret) {
		t.Fatalf("CRITICAL: raw session secret leaked to log file!\nContent: %s", contentStr)
	}

	// Should contain redacted representation
	if !strings.Contains(contentStr, "MoodleSession=sup***890") {
		t.Errorf("expected redacted cookie in log, got: %s", contentStr)
	}
}

func TestLogger_ConcurrentLogging(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "concurrent.txt")

	l, err := logger.New(logPath)
	if err != nil {
		t.Fatalf("failed creating logger: %v", err)
	}
	defer func() { _ = l.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(taskID int) {
			defer wg.Done()
			l.LogTaskStart(taskID, "https://example.com/file.pdf")
			l.LogTaskSuccess(taskID, "file.pdf", 500)
		}(i)
	}

	wg.Wait()
}

func TestRedactCookie(t *testing.T) {
	tests := []struct {
		input    string
		contains string
		excludes string
	}{
		{
			input:    "",
			contains: "(none provided)",
			excludes: "",
		},
		{
			input:    "MoodleSession=abcdef1234567890ghijkl",
			contains: "MoodleSession=abc***jkl (len: 22)",
			excludes: "1234567890",
		},
		{
			input:    "MoodleSession=12345; user=student",
			contains: "MoodleSession=*** (len: 5)",
			excludes: "12345",
		},
	}

	for _, tc := range tests {
		redacted := logger.RedactCookie(tc.input)
		if !strings.Contains(redacted, tc.contains) {
			t.Errorf("input '%s': expected to contain '%s', got '%s'", tc.input, tc.contains, redacted)
		}
		if tc.excludes != "" && strings.Contains(redacted, tc.excludes) {
			t.Errorf("input '%s': leaked secret '%s' in redacted output '%s'", tc.input, tc.excludes, redacted)
		}
	}
}
