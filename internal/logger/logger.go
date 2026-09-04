// Package logger provides structured, privacy-safe debug logging for GoDownloader
// built on top of the standard log/slog library.
package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// SessionCookie is a typed string wrapper implementing slog.LogValuer
// to guarantee session tokens are automatically redacted by the slog runtime.
type SessionCookie string

// LogValue implements slog.LogValuer to prevent secret leakage in logs.
func (c SessionCookie) LogValue() slog.Value {
	return slog.StringValue(RedactCookie(string(c)))
}

// String returns the safe redacted representation of the session cookie.
func (c SessionCookie) String() string {
	return RedactCookie(string(c))
}

// Logger encapsulates an *slog.Logger writing structured, redacted logs to disk.
type Logger struct {
	mu       sync.Mutex
	file     *os.File
	filePath string
	slog     *slog.Logger
}

// New creates and initializes a Logger writing structured text logs to targetPath.
func New(targetPath string) (*Logger, error) {
	if strings.TrimSpace(targetPath) == "" {
		targetPath = "godownloader_debug.txt"
	}

	f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	header := fmt.Sprintf(
		"================================================================================\n"+
			" GoDownloader Debug Log - %s\n"+
			"================================================================================\n\n",
		time.Now().Format("2006-01-02 15:04:05 MST"),
	)
	_, _ = f.WriteString(header)

	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Redact any attribute key indicating cookies or auth tokens
			k := strings.ToLower(a.Key)
			if strings.Contains(k, "cookie") || strings.Contains(k, "token") || strings.Contains(k, "session") {
				if str, ok := a.Value.Any().(string); ok {
					return slog.String(a.Key, RedactCookie(str))
				}
			}
			return a
		},
	}

	handler := slog.NewTextHandler(f, opts)
	sl := slog.New(handler)

	return &Logger{
		file:     f,
		filePath: targetPath,
		slog:     sl,
	}, nil
}

// Slog returns the underlying *slog.Logger instance.
func (l *Logger) Slog() *slog.Logger {
	if l == nil {
		return nil
	}
	return l.slog
}

// FilePath returns the location of the log file.
func (l *Logger) FilePath() string {
	if l == nil {
		return ""
	}
	return l.filePath
}

// Close flushes and closes the underlying log file.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

// Info logs at LevelInfo using structured attributes.
func (l *Logger) Info(msg string, args ...any) {
	if l == nil || l.slog == nil {
		return
	}
	l.slog.Info(msg, args...)
}

// Error logs at LevelError using structured attributes.
func (l *Logger) Error(msg string, args ...any) {
	if l == nil || l.slog == nil {
		return
	}
	l.slog.Error(msg, args...)
}

// Debug logs at LevelDebug using structured attributes.
func (l *Logger) Debug(msg string, args ...any) {
	if l == nil || l.slog == nil {
		return
	}
	l.slog.Debug(msg, args...)
}

// Warn logs at LevelWarn using structured attributes.
func (l *Logger) Warn(msg string, args ...any) {
	if l == nil || l.slog == nil {
		return
	}
	l.slog.Warn(msg, args...)
}

// Log logs a message with a specific level and attributes.
func (l *Logger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	if l == nil || l.slog == nil {
		return
	}
	l.slog.Log(ctx, level, msg, args...)
}

// With returns a new Logger with the specified attributes appended.
func (l *Logger) With(args ...any) *Logger {
	if l == nil {
		return nil
	}
	return &Logger{
		file:     l.file,
		filePath: l.filePath,
		slog:     l.slog.With(args...),
	}
}

// Printf logs a formatted message using LevelInfo.
func (l *Logger) Printf(format string, args ...any) {
	if l == nil || l.slog == nil {
		return
	}
	l.slog.Info(fmt.Sprintf(format, args...))
}

// LogTaskStart records the beginning of a download task with structured attributes.
func (l *Logger) LogTaskStart(taskID int, rawURL string) {
	l.Info("task queued",
		slog.Int("task_id", taskID),
		slog.String("url", rawURL),
	)
}

// LogTaskRedirect records an HTTP redirect with structured attributes.
func (l *Logger) LogTaskRedirect(taskID int, fromURL, toURL string, status int) {
	l.Info("task redirect",
		slog.Int("task_id", taskID),
		slog.Int("status", status),
		slog.String("from_url", fromURL),
		slog.String("to_url", toURL),
	)
}

// LogTaskResponse records HTTP response details with structured attributes.
func (l *Logger) LogTaskResponse(taskID int, status int, contentType string, contentLength int64, disposition string) {
	l.Info("task response",
		slog.Int("task_id", taskID),
		slog.Int("status", status),
		slog.String("content_type", contentType),
		slog.Int64("content_length", contentLength),
		slog.String("disposition", disposition),
	)
}

// LogTaskSuccess records completion of a task with structured attributes.
func (l *Logger) LogTaskSuccess(taskID int, filename string, bytesRead int64) {
	l.Info("task success",
		slog.Int("task_id", taskID),
		slog.String("filename", filename),
		slog.Int64("bytes", bytesRead),
	)
}

// LogTaskError records a task failure with diagnostic explanation and structured attributes.
func (l *Logger) LogTaskError(taskID int, rawURL string, err error) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	l.Error("task error",
		slog.Int("task_id", taskID),
		slog.String("url", rawURL),
		slog.String("error", errMsg),
	)
}

// RedactCookie creates a safe, obfuscated summary of a session cookie for logging,
// ensuring secrets are never persisted to disk.
func RedactCookie(rawCookie string) string {
	trimmed := strings.TrimSpace(rawCookie)
	if trimmed == "" {
		return "(none provided)"
	}

	parts := strings.Split(trimmed, ";")
	var redactedParts []string

	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		name := kv[0]
		if len(kv) == 1 {
			redactedParts = append(redactedParts, fmt.Sprintf("%s=[HIDDEN, len: %d]", name, len(name)))
			continue
		}
		val := kv[1]
		if len(val) <= 6 {
			redactedParts = append(redactedParts, fmt.Sprintf("%s=*** (len: %d)", name, len(val)))
		} else {
			prefix := val[:3]
			suffix := val[len(val)-3:]
			redactedParts = append(redactedParts, fmt.Sprintf("%s=%s***%s (len: %d)", name, prefix, suffix, len(val)))
		}
	}

	return strings.Join(redactedParts, "; ")
}
