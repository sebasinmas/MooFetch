package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var integrationPDF = []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF")

// cookieRecorder is an httptest server that records the exact Cookie request
// header(s) of every request and serves a small valid PDF.
type cookieRecorder struct {
	*httptest.Server
	mu      sync.Mutex
	cookies [][]string
}

func newCookieRecorder(t *testing.T) *cookieRecorder {
	t.Helper()
	rec := &cookieRecorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.cookies = append(rec.cookies, append([]string(nil), r.Header.Values("Cookie")...))
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(integrationPDF)
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (c *cookieRecorder) requests() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]string(nil), c.cookies...)
}

// runExitCode drives the real CLI path (Cobra + realMain) and returns the
// POSIX exit code, exactly as the binary would.
func runExitCode(t *testing.T, args ...string) int {
	t.Helper()
	resetFlags(t)
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs(args)
	return realMain(context.Background(), io.Discard)
}

func TestCookieFormatting_EndToEnd_HeaderSentToServer(t *testing.T) {
	tests := []struct {
		name  string
		input string // value of --cookie; ignored when env is set
		env   string // MOODLE_SESSION; when non-empty --cookie is not passed
		want  string // exact Cookie header expected on the wire
	}{
		{name: "double-quoted value", input: `MoodleSession="asasdoaskd"`, want: "MoodleSession=asasdoaskd"},
		{name: "quoted bare token", input: `"asasdoaskd"`, want: "MoodleSession=asasdoaskd"},
		{name: "bare token", input: `asoidjasiod`, want: "MoodleSession=asoidjasiod"},
		{name: "Cookie: header prefix", input: `Cookie: MoodleSession=abc`, want: "MoodleSession=abc"},
		{name: "lowercase cookie: prefix", input: `cookie:MoodleSession=abc`, want: "MoodleSession=abc"},
		{name: "Cookie: prefix with quoted value", input: `Cookie: MoodleSession="abc"`, want: "MoodleSession=abc"},
		{name: "single quotes around pair", input: `'MoodleSession=abc'`, want: "MoodleSession=abc"},
		{name: "single quotes around value", input: `MoodleSession='abc'`, want: "MoodleSession=abc"},
		{name: "surrounding whitespace", input: "  \t MoodleSession=abc \n ", want: "MoodleSession=abc"},
		{name: "whitespace around equals", input: `MoodleSession = abc`, want: "MoodleSession=abc"},
		{name: "colon separator with quotes", input: `MoodleSession: "abc"`, want: "MoodleSession=abc"},
		{name: "quoted JSON-style colon", input: `"MoodleSession": "abc"`, want: "MoodleSession=abc"},
		{name: "duplicated prefix", input: `MoodleSession=MoodleSession=abc`, want: "MoodleSession=abc"},
		{name: "duplicated prefix with quotes", input: `MoodleSession="MoodleSession=abc"`, want: "MoodleSession=abc"},
		{name: "suffixed cookie name is preserved", input: `MoodleSessionxyz=abc`, want: "MoodleSessionxyz=abc"},
		{name: "suffixed cookie name with Cookie prefix and quotes", input: `Cookie: "MoodleSessionufro=abc"`, want: "MoodleSessionufro=abc"},
		{name: "case-insensitive session name keeps user casing", input: `moodlesession=abc`, want: "moodlesession=abc"},
		{name: "env var bare token", env: `abc`, want: "MoodleSession=abc"},
		{name: "env var quoted pair", env: `"MoodleSession=abc"`, want: "MoodleSession=abc"},
		{name: "env var with Cookie prefix and whitespace", env: "  Cookie: MoodleSession=abc  ", want: "MoodleSession=abc"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withAuthFlags(t, "", "", fakeCookies{})
			t.Setenv("MOODLE_SESSION", tc.env)
			srv := newCookieRecorder(t)
			out := t.TempDir()

			args := []string{"run", "--headless", "-u", srv.URL + "/files/Apunte.pdf", "-o", out}
			if tc.env == "" {
				args = append(args, "--cookie", tc.input)
			}
			if code := runExitCode(t, args...); code != ExitSuccess {
				t.Fatalf("exit code = %d, want %d", code, ExitSuccess)
			}

			reqs := srv.requests()
			if len(reqs) != 1 {
				t.Fatalf("server saw %d requests, want 1", len(reqs))
			}
			if len(reqs[0]) != 1 || reqs[0][0] != tc.want {
				t.Errorf("Cookie header = %q, want exactly [%q]", reqs[0], tc.want)
			}
			if strings.ContainsAny(reqs[0][0], "\"' \t\r\n") {
				t.Errorf("Cookie header %q contains quotes or whitespace", reqs[0][0])
			}

			got, err := os.ReadFile(filepath.Join(out, "Apunte.pdf"))
			if err != nil {
				t.Fatalf("file not saved: %v", err)
			}
			if string(got) != string(integrationPDF) {
				t.Errorf("saved bytes differ from served PDF")
			}
			if m, _ := filepath.Glob(filepath.Join(out, "*.moofetch.part")); len(m) != 0 {
				t.Errorf("leftover part files: %v", m)
			}
		})
	}
}

func TestCookieFormatting_EndToEnd_EmptyInputNeverReachesServer(t *testing.T) {
	tests := []struct {
		name     string
		args     []string // extra args after the common ones
		env      string
		wantCode int
	}{
		{name: "empty --cookie and no env", args: []string{"--cookie", ""}, wantCode: ExitUsageErr},
		{name: "no --cookie and no env", wantCode: ExitUsageErr},
		{name: "whitespace-only --cookie", args: []string{"--cookie", "   \t "}, wantCode: ExitUsageErr},
		{name: "quotes-only --cookie", args: []string{"--cookie", `""`}, wantCode: ExitUsageErr},
		{name: "Cookie: prefix only", args: []string{"--cookie", "Cookie:"}, wantCode: ExitUsageErr},
		{name: "whitespace-only env var", env: "  \n", wantCode: ExitUsageErr},
		{name: "quotes-only env var", env: `''`, wantCode: ExitUsageErr},
		{
			// Normalization does not strip interior control characters; the Go
			// HTTP client must refuse the invalid header value, so the request
			// never reaches the server and the run fails as a general error.
			name:     "embedded newline is rejected by the client",
			args:     []string{"--cookie", "MoodleSession=ab\r\nX-Injected: 1"},
			wantCode: ExitGeneralErr,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withAuthFlags(t, "", "", fakeCookies{})
			t.Setenv("MOODLE_SESSION", tc.env)
			srv := newCookieRecorder(t)
			out := t.TempDir()

			args := append([]string{"run", "--headless", "-u", srv.URL + "/files/Apunte.pdf", "-o", out}, tc.args...)
			if code := runExitCode(t, args...); code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d", code, tc.wantCode)
			}
			if n := len(srv.requests()); n != 0 {
				t.Errorf("server received %d request(s) (cookies %q); none expected", n, srv.requests())
			}
			if entries, _ := os.ReadDir(out); len(entries) != 0 {
				t.Errorf("output dir must stay empty, got %v", entries)
			}
		})
	}
}

// TestCookieFormatting_ExplicitFlagBeatsEnv checks the precedence --cookie > env
// on the wire, not just in resolveHeadlessCookie.
func TestCookieFormatting_ExplicitFlagBeatsEnv(t *testing.T) {
	withAuthFlags(t, "", "", fakeCookies{})
	t.Setenv("MOODLE_SESSION", "fromenv")
	srv := newCookieRecorder(t)
	out := t.TempDir()

	if code := runExitCode(t, "run", "--headless", "-u", srv.URL+"/a.pdf", "-o", out, "-k", `"fromflag"`); code != ExitSuccess {
		t.Fatalf("exit code = %d", code)
	}
	reqs := srv.requests()
	if len(reqs) != 1 || len(reqs[0]) != 1 || reqs[0][0] != "MoodleSession=fromflag" {
		t.Errorf("Cookie header = %q, want [MoodleSession=fromflag]", reqs)
	}
}

// TestCookieFormatting_BrowserDetectedCookieOnWire runs the --domain detection
// path (fake provider, no real browser) and checks both provider output
// shapes (bare value, Name=value) arrive well-formed at the server.
func TestCookieFormatting_BrowserDetectedCookieOnWire(t *testing.T) {
	tests := []struct {
		name     string
		provided string
		want     string
	}{
		{"bare value gets default name", "abc123", "MoodleSession=abc123"},
		{"provider returns Name=value", "MoodleSessionxyz=abc123", "MoodleSessionxyz=abc123"},
		{"provider value with padding whitespace", "  abc123 \n", "MoodleSession=abc123"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withAuthFlags(t, "", "campus.example.cl", fakeCookies{val: tc.provided})
			t.Setenv("MOODLE_SESSION", "")
			srv := newCookieRecorder(t)
			out := t.TempDir()

			// resetFlags (inside runExitCode) restores --domain, so pass it explicitly.
			code := runExitCode(t, "run", "--headless", "--domain", "campus.example.cl",
				"-u", srv.URL+"/a.pdf", "-o", out)
			if code != ExitSuccess {
				t.Fatalf("exit code = %d", code)
			}
			reqs := srv.requests()
			if len(reqs) != 1 || len(reqs[0]) != 1 || reqs[0][0] != tc.want {
				t.Errorf("Cookie header = %q, want [%q]", reqs, tc.want)
			}
		})
	}
}

// TestCookieFormatting_EmptyValue_IsUsageError carries the assertion we would want
// once a name-without-value cookie is treated like empty input.
func TestCookieFormatting_EmptyValue_IsUsageError(t *testing.T) {
	withAuthFlags(t, "", "", fakeCookies{})
	t.Setenv("MOODLE_SESSION", "")
	srv := newCookieRecorder(t)
	code := runExitCode(t, "run", "--headless", "-u", srv.URL+"/a.pdf", "-o", t.TempDir(), "-k", "MoodleSession=")
	if code != ExitUsageErr || len(srv.requests()) != 0 {
		t.Errorf("want usage error and no request, got code %d, %d requests", code, len(srv.requests()))
	}
}
