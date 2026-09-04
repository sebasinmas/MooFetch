package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"godownloader/internal/logger"
)

// TestSessionCookie_LogValuer_PrivacyTDT validates that SessionCookie implementing slog.LogValuer
// guarantees that raw secret values NEVER leak into slog handler outputs (Text and JSON),
// and always replace sensitive tokens with masked representations (***).
func TestSessionCookie_LogValuer_PrivacyTDT(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		rawCookie     string
		forbiddenStrs []string
		mustContain   []string
	}{
		{
			name:          "Standard Moodle long session token",
			rawCookie:     "MoodleSession=SECRET123456789",
			forbiddenStrs: []string{"SECRET123456789", "RET1234"},
			mustContain:   []string{"MoodleSession=", "SEC***789", "len: 15"},
		},
		{
			name:          "Short session token (<= 6 chars)",
			rawCookie:     "MoodleSession=SECRET",
			forbiddenStrs: []string{"SECRET"},
			mustContain:   []string{"MoodleSession=***", "len: 6"},
		},
		{
			name:          "Very short session token",
			rawCookie:     "MoodleSession=123",
			forbiddenStrs: []string{"123"},
			mustContain:   []string{"MoodleSession=***", "len: 3"},
		},
		{
			name:          "Multiple cookies with secrets and preferences",
			rawCookie:     "MoodleSession=SECRET123456; MOODLEID1_=%25D1%25B2; user_theme=dark",
			forbiddenStrs: []string{"SECRET123456", "%25D1%25B2"},
			mustContain:   []string{"MoodleSession=", "***", "user_theme="},
		},
		{
			name:          "Empty cookie value",
			rawCookie:     "",
			forbiddenStrs: nil,
			mustContain:   []string{"(none provided)"},
		},
		{
			name:          "Whitespace only cookie value",
			rawCookie:     "   \t  \n ",
			forbiddenStrs: nil,
			mustContain:   []string{"(none provided)"},
		},
		{
			name:          "Malformed cookie token without key-value separator",
			rawCookie:     "MALFORMED_KEY_NO_VAL",
			forbiddenStrs: nil,
			mustContain:   []string{"MALFORMED_KEY_NO_VAL=[HIDDEN, len: 20]"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cookie := logger.SessionCookie(tc.rawCookie)

			// Subtest 1: Verify using slog TextHandler writing to bytes.Buffer
			t.Run("TextHandler", func(t *testing.T) {
				t.Parallel()
				var buf bytes.Buffer
				h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
				l := slog.New(h)

				l.Info("testing cookie privacy", slog.Any("cookie", cookie))

				output := buf.String()

				// Validate forbidden secrets are absent
				for _, forbidden := range tc.forbiddenStrs {
					if strings.Contains(output, forbidden) {
						t.Errorf("privacy breach: TextHandler output leaked secret %q!\nLog: %s", forbidden, output)
					}
				}

				// Validate required obfuscation markers are present
				for _, expected := range tc.mustContain {
					if !strings.Contains(output, expected) {
						t.Errorf("TextHandler output missing expected marker %q.\nLog: %s", expected, output)
					}
				}
			})

			// Subtest 2: Verify using slog JSONHandler writing to bytes.Buffer
			t.Run("JSONHandler", func(t *testing.T) {
				t.Parallel()
				var buf bytes.Buffer
				h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
				l := slog.New(h)

				l.Info("testing cookie privacy json", slog.Any("auth_cookie", cookie))

				output := buf.String()

				// Parse json to ensure valid format
				var parsed map[string]any
				if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
					t.Fatalf("JSONHandler emitted invalid JSON: %v\nOutput: %s", err, output)
				}

				for _, forbidden := range tc.forbiddenStrs {
					if strings.Contains(output, forbidden) {
						t.Errorf("privacy breach: JSONHandler output leaked secret %q!\nLog: %s", forbidden, output)
					}
				}

				for _, expected := range tc.mustContain {
					if !strings.Contains(output, expected) {
						t.Errorf("JSONHandler output missing expected marker %q.\nLog: %s", expected, output)
					}
				}
			})
		})
	}
}

// TestSessionCookie_StringerMethod verifies fmt.Stringer implementation on SessionCookie.
func TestSessionCookie_StringerMethod(t *testing.T) {
	t.Parallel()

	rawSecret := "SUPER_CONFIDENTIAL_TOKEN_999"
	cookie := logger.SessionCookie("MoodleSession=" + rawSecret)

	str := cookie.String()
	if strings.Contains(str, rawSecret) {
		t.Fatalf("cookie.String() leaked secret token: %s", str)
	}
	if !strings.Contains(str, "***") {
		t.Errorf("cookie.String() expected to contain '***', got: %s", str)
	}
}

// TestLogger_FileEndToEnd_Privacy verifies that an initialized *logger.Logger writes
// redacted output to disk across all log levels and attribute injection mechanisms.
func TestLogger_FileEndToEnd_Privacy(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "privacy_e2e.txt")

	l, err := logger.New(logPath)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	secret1 := "MOODLE_SECRET_TOKEN_ALPHA"
	secret2 := "BEARER_SECRET_TOKEN_BETA"
	secret3 := "SESSION_SECRET_TOKEN_GAMMA"

	// 1. Log using typed SessionCookie
	l.Info("auth established", slog.Any("cookie", logger.SessionCookie("MoodleSession="+secret1)))

	// 2. Log using raw string attribute key containing 'cookie' (triggers ReplaceAttr redaction)
	l.Debug("request sent", slog.String("request_cookie", "MoodleSession="+secret2))

	// 3. Log using logger.With child logger
	child := l.With(slog.String("session_token", "MoodleSession="+secret3))
	child.Warn("token refreshed")

	// 4. Log helper methods
	l.LogTaskStart(101, "https://campusvirtual.ufro.cl/view.php?id=101")
	l.LogTaskError(101, "https://campusvirtual.ufro.cl/view.php?id=101", fmt.Errorf("auth error"))

	if err := l.Close(); err != nil {
		t.Fatalf("failed closing logger: %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed reading log file: %v", err)
	}

	contentStr := string(content)

	// Assert NO secrets exist in the file
	for _, sec := range []string{secret1, secret2, secret3} {
		if strings.Contains(contentStr, sec) {
			t.Fatalf("CRITICAL SECURITY VULNERABILITY: plain secret %q leaked to disk!\nLog:\n%s", sec, contentStr)
		}
	}

	// Assert obfuscated values exist
	if !strings.Contains(contentStr, "***") {
		t.Errorf("expected obfuscation asterisks '***' in log content, got:\n%s", contentStr)
	}
}

// TestLogger_WriteAndRead validates basic write operations, log headers, and formatted messages.
func TestLogger_WriteAndRead(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "test_debug.txt")

	l, err := logger.New(logPath)
	if err != nil {
		t.Fatalf("failed creating logger: %v", err)
	}

	l.Printf("System initialized with %d workers", 5)
	l.LogTaskStart(1, "https://campusvirtual.ufro.cl/test.pdf")
	l.LogTaskRedirect(1, "https://campusvirtual.ufro.cl/view.php", "https://campusvirtual.ufro.cl/pluginfile.php", 303)
	l.LogTaskResponse(1, 200, "application/pdf", 2048, `attachment; filename="test.pdf"`)
	l.LogTaskSuccess(1, "test.pdf", 2048)

	if err := l.Close(); err != nil {
		t.Fatalf("failed closing logger: %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed reading log file: %v", err)
	}

	contentStr := string(content)
	checks := []string{
		"GoDownloader Debug Log",
		"System initialized with 5 workers",
		"task_id=1",
		"status=303",
		"status=200",
		`filename=test.pdf`,
	}

	for _, check := range checks {
		if !strings.Contains(contentStr, check) {
			t.Errorf("expected %q in log file, got:\n%s", check, contentStr)
		}
	}
}

// TestLogger_TypedAttributesAndSlogIntegration verifies slog levels and typed attribute logging.
func TestLogger_TypedAttributesAndSlogIntegration(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "typed_attrs.txt")

	l, err := logger.New(logPath)
	if err != nil {
		t.Fatalf("failed creating logger: %v", err)
	}

	l.Info("download finished", slog.Int("task_id", 42), slog.Int64("bytes", 999999))
	l.Debug("debug message", slog.String("component", "transport"))
	l.Warn("warning event", slog.String("reason", "slow server"))
	l.Error("failure event", slog.String("err_code", "TIMEOUT"))
	l.Log(context.Background(), slog.LevelInfo, "explicit level message", slog.Bool("active", true))

	if l.Slog() == nil {
		t.Errorf("expected Slog() to return non-nil *slog.Logger")
	}
	if l.FilePath() != logPath {
		t.Errorf("expected FilePath %q, got %q", logPath, l.FilePath())
	}

	if err := l.Close(); err != nil {
		t.Fatalf("failed closing logger: %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed reading log file: %v", err)
	}

	contentStr := string(content)
	expectedSubstrings := []string{
		`msg="download finished"`,
		"task_id=42",
		"bytes=999999",
		"component=transport",
		"reason=\"slow server\"",
		"err_code=TIMEOUT",
		"active=true",
	}

	for _, exp := range expectedSubstrings {
		if !strings.Contains(contentStr, exp) {
			t.Errorf("expected log to contain %q, but missing.\nFull log:\n%s", exp, contentStr)
		}
	}
}

// TestLogger_ConcurrentLogging ensures thread-safety under heavy concurrent load without data corruption.
func TestLogger_ConcurrentLogging(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "concurrent.txt")

	l, err := logger.New(logPath)
	if err != nil {
		t.Fatalf("failed creating logger: %v", err)
	}
	defer func() { _ = l.Close() }()

	const goroutines = 25
	const iterationsPerGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterationsPerGoroutine; j++ {
				taskID := workerID*1000 + j
				l.LogTaskStart(taskID, "https://example.com/file.pdf")
				l.Info("progress", slog.Int("worker", workerID), slog.Int("iter", j))
				l.LogTaskSuccess(taskID, "file.pdf", int64(j*100))
			}
		}(i)
	}

	wg.Wait()

	if err := l.Close(); err != nil {
		t.Fatalf("failed closing logger: %v", err)
	}

	stat, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("failed stating log file: %v", err)
	}
	if stat.Size() == 0 {
		t.Errorf("expected non-empty log file from concurrent logging")
	}
}

// TestRedactCookie_EdgeCasesTDT tests pure RedactCookie function behavior on multiple edge cases.
func TestRedactCookie_EdgeCasesTDT(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		contains string
		excludes string
	}{
		{
			name:     "Empty string",
			input:    "",
			contains: "(none provided)",
			excludes: "",
		},
		{
			name:     "Whitespaces only",
			input:    "   \n\t ",
			contains: "(none provided)",
			excludes: "",
		},
		{
			name:     "Standard Moodle long cookie",
			input:    "MoodleSession=abcdef1234567890ghijkl",
			contains: "MoodleSession=abc***jkl (len: 22)",
			excludes: "1234567890",
		},
		{
			name:     "Short token under 6 characters",
			input:    "MoodleSession=12345; user=student",
			contains: "MoodleSession=*** (len: 5)",
			excludes: "12345",
		},
		{
			name:     "Cookie without equals sign",
			input:    "MALFORMED_NO_EQUALS",
			contains: "MALFORMED_NO_EQUALS=[HIDDEN, len: 19]",
			excludes: "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			redacted := logger.RedactCookie(tc.input)
			if !strings.Contains(redacted, tc.contains) {
				t.Errorf("input %q: expected to contain %q, got %q", tc.input, tc.contains, redacted)
			}
			if tc.excludes != "" && strings.Contains(redacted, tc.excludes) {
				t.Errorf("input %q: leaked secret %q in redacted output %q", tc.input, tc.excludes, redacted)
			}
		})
	}
}

// TestLogger_NilSafeGuards ensures calling logger methods on nil pointer does not panic.
func TestLogger_NilSafeGuards(t *testing.T) {
	t.Parallel()

	var l *logger.Logger

	// Should not panic on nil receiver
	l.Info("nil test")
	l.Debug("nil test")
	l.Warn("nil test")
	l.Error("nil test")
	l.Printf("nil test %d", 1)
	l.Log(context.Background(), slog.LevelInfo, "nil test")
	l.LogTaskStart(1, "url")
	l.LogTaskRedirect(1, "u1", "u2", 303)
	l.LogTaskResponse(1, 200, "pdf", 100, "disp")
	l.LogTaskSuccess(1, "file", 100)
	l.LogTaskError(1, "url", fmt.Errorf("err"))

	if l.Slog() != nil {
		t.Errorf("expected Slog() to return nil on nil logger")
	}
	if l.FilePath() != "" {
		t.Errorf("expected FilePath() to return empty string on nil logger")
	}
	if l.With(slog.Int("k", 1)) != nil {
		t.Errorf("expected With() to return nil on nil logger")
	}
	if err := l.Close(); err != nil {
		t.Errorf("expected Close() on nil logger to return nil, got: %v", err)
	}
}
