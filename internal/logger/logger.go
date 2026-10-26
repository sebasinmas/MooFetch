// Package logger provides structured, privacy-safe debug logging for MooFetch
// built on top of the standard log/slog library.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
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

// Token is a typed string wrapper for Moodle web-service tokens. Like
// SessionCookie it implements slog.LogValuer so it can never reach a log in
// clear text: only its length is shown.
type Token string

// LogValue implements slog.LogValuer to prevent secret leakage in logs.
func (t Token) LogValue() slog.Value {
	return slog.StringValue(RedactToken(string(t)))
}

// String returns the safe redacted representation of the token.
func (t Token) String() string {
	return RedactToken(string(t))
}

// GoString keeps %#v from printing the secret.
func (t Token) GoString() string {
	return RedactToken(string(t))
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
		targetPath = "moofetch_debug.txt"
	}

	f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	header := fmt.Sprintf(
		"================================================================================\n"+
			" MooFetch Debug Log - %s\n"+
			"================================================================================\n\n",
		time.Now().Format("2006-01-02 15:04:05 MST"),
	)
	_, _ = f.WriteString(header)

	return &Logger{
		file:     f,
		filePath: targetPath,
		slog:     slog.New(slog.NewTextHandler(f, handlerOptions())),
	}, nil
}

// NewWriter creates a Logger that writes the same redacted structured text to
// w instead of a file. Close does not close w. It is meant for tests and for
// callers that already own the destination.
func NewWriter(w io.Writer) *Logger {
	return &Logger{slog: slog.New(slog.NewTextHandler(w, handlerOptions()))}
}

// handlerOptions builds the slog options shared by every Logger: attributes
// whose key looks secret are masked, and every other text (message, string
// attributes and error values) is scrubbed with RedactURL as a safety net.
func handlerOptions() *slog.HandlerOptions {
	return &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			return redactAttr(a)
		},
	}
}

func redactAttr(a slog.Attr) slog.Attr {
	k := strings.ToLower(a.Key)
	str, isString := a.Value.Any().(string)
	switch {
	case isString && (strings.Contains(k, "password") || strings.Contains(k, "token")):
		return slog.String(a.Key, RedactToken(str))
	case isString && (strings.Contains(k, "cookie") || strings.Contains(k, "session")):
		return slog.String(a.Key, RedactCookie(str))
	case isString:
		return slog.String(a.Key, RedactURL(str))
	}
	if err, ok := a.Value.Any().(error); ok && err != nil {
		return slog.String(a.Key, RedactURL(err.Error()))
	}
	return a
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
		slog.String("url", RedactURL(rawURL)),
	)
}

// LogTaskRedirect records an HTTP redirect with structured attributes.
func (l *Logger) LogTaskRedirect(taskID int, fromURL, toURL string, status int) {
	l.Info("task redirect",
		slog.Int("task_id", taskID),
		slog.Int("status", status),
		slog.String("from_url", RedactURL(fromURL)),
		slog.String("to_url", RedactURL(toURL)),
	)
}

// LogTaskResponse records HTTP response details with structured attributes.
func (l *Logger) LogTaskResponse(taskID int, status int, contentType string, contentLength int64, disposition string) {
	l.Info("task response",
		slog.Int("task_id", taskID),
		slog.Int("status", status),
		slog.String("content_type", contentType),
		slog.Int64("content_length", contentLength),
		slog.String("disposition", RedactURL(disposition)),
	)
}

// LogTaskSuccess records completion of a task with structured attributes.
func (l *Logger) LogTaskSuccess(taskID int, filename string, bytesRead int64) {
	l.Info("task success",
		slog.Int("task_id", taskID),
		slog.String("filename", RedactURL(filename)),
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
		slog.String("url", RedactURL(rawURL)),
		slog.String("error", RedactURL(errMsg)),
	)
}

// minRevealLen is the longest cookie value that is fully masked; longer values
// keep 3 chars at each end, which never exposes most of the secret.
const minRevealLen = 12

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
		if len(val) <= minRevealLen {
			redactedParts = append(redactedParts, fmt.Sprintf("%s=*** (len: %d)", name, len(val)))
		} else {
			prefix := val[:3]
			suffix := val[len(val)-3:]
			redactedParts = append(redactedParts, fmt.Sprintf("%s=%s***%s (len: %d)", name, prefix, suffix, len(val)))
		}
	}

	if len(redactedParts) == 0 {
		return "(none provided)"
	}
	return strings.Join(redactedParts, "; ")
}

// RedactToken hides a web-service token completely, revealing only its length.
func RedactToken(raw string) string {
	if raw == "" {
		return "(none provided)"
	}
	return fmt.Sprintf("***(len: %d)", len(raw))
}

const redacted = "REDACTED"

var (
	// secretParam matches token=..., wstoken=... and password=... when they
	// appear as a query/form parameter or at the start of a text.
	secretParam = regexp.MustCompile(`(?i)(^|[?&;\s"'(\[{,])((?:ws)?token|password)=([^&\s"'<>)\]}#,]*)`)
	// userinfo matches user[:password]@ right after a URL scheme.
	userinfo = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s"'@?#]*@`)
)

// RedactURL removes secrets from a URL or from any text containing URLs: the
// values of the token, wstoken and password parameters are replaced and
// user:password@ credentials are dropped. It is idempotent and safe to apply
// to texts that hold no URL at all.
func RedactURL(raw string) string {
	if raw == "" {
		return raw
	}
	out := userinfo.ReplaceAllString(raw, "${1}")
	return secretParam.ReplaceAllString(out, "${1}${2}="+redacted)
}
