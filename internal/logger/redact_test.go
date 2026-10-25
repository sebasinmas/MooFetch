package logger_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sebasinmas/MooFetch/internal/logger"
)

const (
	secretTok  = "TOKSECRET0123456789abcdef"
	secretPass = "PASSSECRET-xyz!"
)

func TestToken_LogValuerStringerAndGoString(t *testing.T) {
	tok := logger.Token(secretTok)
	want := fmt.Sprintf("***(len: %d)", len(secretTok))

	if got := tok.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got := tok.GoString(); got != want {
		t.Errorf("GoString() = %q", got)
	}
	for _, format := range []string{"%s", "%v", "%+v", "%#v", "%q"} {
		if out := fmt.Sprintf(format, tok); strings.Contains(out, secretTok) {
			t.Errorf("format %s leaks the token: %s", format, out)
		}
	}

	var buf bytes.Buffer
	l := logger.NewWriter(&buf)
	l.Info("typed", "t", tok, slog.Any("u", tok))
	out := buf.String()
	if strings.Contains(out, secretTok) || !strings.Contains(out, want) {
		t.Errorf("log = %s", out)
	}
}

func TestRedactToken(t *testing.T) {
	if got := logger.RedactToken(""); got != "(none provided)" {
		t.Errorf("empty = %q", got)
	}
	if got := logger.RedactToken("a"); got != "***(len: 1)" {
		t.Errorf("short = %q", got)
	}
	long := strings.Repeat("z", 40)
	got := logger.RedactToken(long)
	if got != "***(len: 40)" || strings.Contains(got, "z") {
		t.Errorf("long = %q", got)
	}
}

func TestRedactURL(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"no secrets", "https://c.cl/webservice/pluginfile.php/1/a.pdf?forcedownload=1", "https://c.cl/webservice/pluginfile.php/1/a.pdf?forcedownload=1"},
		{"token only", "https://c.cl/p/a.pdf?token=ABC", "https://c.cl/p/a.pdf?token=REDACTED"},
		{"token among params", "https://c.cl/p?a=1&token=ABC&b=2", "https://c.cl/p?a=1&token=REDACTED&b=2"},
		{"wstoken", "https://c.cl/p?wstoken=ABC&a=1", "https://c.cl/p?wstoken=REDACTED&a=1"},
		{"password", "https://c.cl/p?username=u&password=hunter2", "https://c.cl/p?username=u&password=REDACTED"},
		{"upper case names", "https://c.cl/p?TOKEN=ABC&WsToken=DEF&Password=GHI", "https://c.cl/p?TOKEN=REDACTED&WsToken=REDACTED&Password=REDACTED"},
		{"escaped value", "https://c.cl/p?token=a%2Bb%2Fc", "https://c.cl/p?token=REDACTED"},
		{"empty value", "https://c.cl/p?token=&a=1", "https://c.cl/p?token=REDACTED&a=1"},
		{"fragment kept", "https://c.cl/p?token=ABC#frag", "https://c.cl/p?token=REDACTED#frag"},
		{"userinfo", "https://user:pass@c.cl/p", "https://c.cl/p"},
		{"userinfo user only", "https://user@c.cl/p", "https://c.cl/p"},
		{"userinfo and token", "https://u:p@c.cl/p?token=ABC", "https://c.cl/p?token=REDACTED"},
		{"inside a Go url.Error", `Get "https://c.cl/p?token=ABC": dial tcp: refused`, `Get "https://c.cl/p?token=REDACTED": dial tcp: refused`},
		{"two urls", "from https://a/x?token=1 to https://b/y?token=2", "from https://a/x?token=REDACTED to https://b/y?token=REDACTED"},
		{"form body", "username=u&password=hunter2&service=x", "username=u&password=REDACTED&service=x"},
		{"text start", "token=ABC", "token=REDACTED"},
		{"space separated", "failed password=hunter2 for user", "failed password=REDACTED for user"},
		{"similar names untouched", "https://c.cl/p?mytoken=1&tokens=2&passwordless=3", "https://c.cl/p?mytoken=1&tokens=2&passwordless=3"},
		{"scheme-relative without credentials", "see mailto:a@b.cl", "see mailto:a@b.cl"},
		{"at sign in path", "https://c.cl/u/a@b.cl/file", "https://c.cl/u/a@b.cl/file"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := logger.RedactURL(tc.in)
			if got != tc.want {
				t.Errorf("RedactURL(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
			if again := logger.RedactURL(got); again != got {
				t.Errorf("not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func TestLogger_TaskHelpersRedactURLs(t *testing.T) {
	var buf bytes.Buffer
	l := logger.NewWriter(&buf)
	u := "https://c.cl/webservice/pluginfile.php/1/a.pdf?token=" + secretTok
	l.LogTaskStart(1, u)
	l.LogTaskRedirect(1, u, "https://other.cl/x?wstoken="+secretTok+"&password="+secretPass, 302)
	l.LogTaskResponse(1, 200, "application/pdf", 10, `attachment; filename="a.pdf"; token=`+secretTok)
	l.LogTaskSuccess(1, "file token="+secretTok+".pdf", 5)
	l.LogTaskError(1, u, fmt.Errorf(`Get %q: refused`, u))
	l.LogTaskError(1, u, nil)

	out := buf.String()
	for _, s := range []string{secretTok, secretPass} {
		if strings.Contains(out, s) {
			t.Fatalf("log leaks %q:\n%s", s[:5], out)
		}
	}
	for _, want := range []string{"task queued", "task redirect", "task response", "task success", "task error", "token=REDACTED"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
}

func TestLogger_SafetyNetOnAnyAttribute(t *testing.T) {
	var buf bytes.Buffer
	l := logger.NewWriter(&buf)
	u := "https://c.cl/p?token=" + secretTok
	l.Info("fetching "+u, "link", u, "token", secretTok, "wstoken", secretTok, "password", secretPass,
		"Cookie", "MoodleSession=abcdefghijklmnopqrstuvwxyz", "err", errors.New("Get \""+u+"\": boom"))
	l.Warn("w", "n", 5, "nilerr", error(nil))
	l.Printf("printf %s", u)
	l.Error("e", slog.Group("g", slog.String("u", u)))
	l.With("base", u).Debug("with")
	l.Log(t.Context(), slog.LevelInfo, "log", "u", u)

	out := buf.String()
	if strings.Contains(out, secretTok) || strings.Contains(out, secretPass) {
		t.Fatalf("log leaks a secret:\n%s", out)
	}
	if strings.Contains(out, "abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("cookie value leaks:\n%s", out)
	}
	if !strings.Contains(out, fmt.Sprintf("***(len: %d)", len(secretTok))) {
		t.Errorf("token-keyed attribute should show its length only:\n%s", out)
	}
	if !strings.Contains(out, "n=5") {
		t.Errorf("non-secret attributes must survive:\n%s", out)
	}
}

func TestLogger_NewWriterAndFileLoggerBehaveTheSame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.txt")
	fl, err := logger.New(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	wl := logger.NewWriter(&buf)
	u := "https://c.cl/p?token=" + secretTok
	fl.LogTaskStart(1, u)
	wl.LogTaskStart(1, u)
	if err := wl.Close(); err != nil {
		t.Errorf("Close on a writer logger = %v", err)
	}
	if wl.FilePath() != "" || wl.Slog() == nil {
		t.Error("writer logger has no file but has a slog logger")
	}
	_ = fl.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secretTok) || !strings.Contains(string(data), "token=REDACTED") {
		t.Errorf("file log = %s", data)
	}
	if strings.Contains(buf.String(), secretTok) {
		t.Errorf("writer log = %s", buf.String())
	}
}
