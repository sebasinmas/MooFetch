package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sebasinmas/MooFetch/internal/auth"
	"github.com/sebasinmas/MooFetch/internal/domain"
	"github.com/sebasinmas/MooFetch/internal/tui"
)

// pipeStdin replaces os.Stdin with a pipe holding content for the test.
func pipeStdin(t *testing.T, content string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(content); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; _ = r.Close() })
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetFlags(t)
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)
	err := Execute(context.Background())
	return buf.String(), err
}

func TestShouldUseHeadless(t *testing.T) {
	resetFlags(t)
	// Under `go test` stdin/stdout are not both terminals -> headless.
	pipeStdin(t, "")
	if !shouldUseHeadless() {
		t.Error("piped stdin must select headless")
	}
	for _, name := range []string{"headless", "plain"} {
		resetFlags(t)
		if err := rootCmd.PersistentFlags().Set(name, "true"); err != nil {
			t.Fatal(err)
		}
		if !shouldUseHeadless() {
			t.Errorf("--%s must select headless", name)
		}
	}
}

func TestAuthOptions(t *testing.T) {
	withAuthFlags(t, "ufro", "", fakeCookies{val: "tok"})
	opts, err := authOptions()
	if err != nil {
		t.Fatal(err)
	}
	if opts.Domain != "campusvirtual.ufro.cl" {
		t.Errorf("domain = %q", opts.Domain)
	}
	if len(opts.Universities) == 0 || opts.Universities[0].Domain == "" {
		t.Errorf("universities not populated: %+v", opts.Universities)
	}
	if _, err := opts.ValidateDomain("bad domain"); !errors.Is(err, auth.ErrInvalidDomain) {
		t.Errorf("ValidateDomain must be auth.ValidateDomain, got %v", err)
	}
	got, err := opts.Detect(context.Background(), "campusvirtual.ufro.cl")
	if err != nil || got != "MoodleSession=tok" {
		t.Errorf("Detect = %q %v", got, err)
	}

	withAuthFlags(t, "nope", "", fakeCookies{})
	if _, err := authOptions(); !errors.Is(err, auth.ErrUnknownUniversity) {
		t.Errorf("want ErrUnknownUniversity, got %v", err)
	}
}

func TestSetupLogger(t *testing.T) {
	form := &tui.FormData{Cookie: "MoodleSession=supersecretvalue", URLs: []string{"a", "b"}}

	if l, p := setupLogger("", 1, ".", form, false); l != nil || p != "" {
		t.Fatalf("empty path must disable logging, got %v %q", l, p)
	}

	for _, demo := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "debug.log")
		l, p := setupLogger(path, 3, "/out", form, demo)
		if l == nil || p != path {
			t.Fatalf("logger not created: %v %q", l, p)
		}
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s := string(data)
		if strings.Contains(s, "supersecretvalue") {
			t.Error("log leaked the raw cookie")
		}
		if !strings.Contains(s, "Total de URLs en cola: 2") {
			t.Errorf("missing queue size in log: %s", s)
		}
		if strings.Contains(s, "MODO DEMO") != demo {
			t.Errorf("demo=%v banner mismatch: %s", demo, s)
		}
	}

	// Unwritable path: warning on stderr and no logger.
	bad := filepath.Join(t.TempDir(), "missing-dir", "x", "debug.log")
	if l, p := setupLogger(bad, 1, ".", form, false); l != nil || p != "" {
		t.Fatalf("unwritable path must yield no logger, got %v %q", l, p)
	}
}

func TestHandleCompletion(t *testing.T) {
	if err := handleCompletion([]domain.Result{{TaskID: 1}}, ""); err != nil {
		t.Fatalf("all ok: %v", err)
	}
	err := handleCompletion([]domain.Result{{TaskID: 1}, {TaskID: 2, Err: errors.New("boom")}}, "")
	var be *BatchError
	if !errors.As(err, &be) || len(be.Results) != 1 || be.Results[0].TaskID != 2 {
		t.Fatalf("want BatchError with only the failed task, got %v", err)
	}
}

func TestInitKernel_PluginSelection(t *testing.T) {
	tasks := []domain.Task{{ID: 1, URL: "https://campusvirtual.ufro.cl/mod/resource/view.php?id=1", Cookie: "c", OutputDir: t.TempDir()}}
	if k := initKernel(2, nil, true); k == nil {
		t.Fatal("demo kernel nil")
	}
	// The moodle plugin handles real URLs; the demo one is used only with --demo.
	k := initKernel(2, nil, false)
	if _, err := k.ResolvePlugin(tasks[0].URL); err != nil {
		t.Fatalf("moodle plugin must resolve URL: %v", err)
	}
}

func TestRealMain_ExitCodes(t *testing.T) {
	// Success: piped demo URL.
	pipeStdin(t, "https://campusvirtual.ufro.cl/mod/resource/view.php?id=1\n")
	resetFlags(t)
	rootCmd.SetArgs([]string{"--demo", "--headless", "-o", t.TempDir()})
	var stderr bytes.Buffer
	if code := realMain(context.Background(), &stderr); code != ExitSuccess {
		t.Fatalf("code %d, stderr %s", code, stderr.String())
	}

	// Usage error: empty stdin yields "no URLs" -> ExitUsageErr and message on stderr.
	pipeStdin(t, "\n")
	resetFlags(t)
	rootCmd.SetArgs([]string{"--headless"})
	stderr.Reset()
	if code := realMain(context.Background(), &stderr); code != ExitUsageErr {
		t.Fatalf("code %d, stderr %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Error:") {
		t.Errorf("error not reported: %q", stderr.String())
	}

	// Cancellation is silent and maps to 130.
	pipeStdin(t, "\n")
	resetFlags(t)
	rootCmd.SetArgs([]string{"--headless"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stderr.Reset()
	if code := realMain(ctx, &stderr); code != ExitInterrupted {
		t.Fatalf("code %d", code)
	}
}

func TestRootCmd_StdinErrors(t *testing.T) {
	t.Setenv("MOODLE_SESSION", "")
	withAuthFlags(t, "", "", fakeCookies{})

	pipeStdin(t, "not a url\n")
	if _, err := runCLI(t, "--headless"); err == nil || !strings.Contains(err.Error(), "URLs válidas") {
		t.Fatalf("want no-URLs error, got %v", err)
	}

	pipeStdin(t, "https://campusvirtual.ufro.cl/mod/resource/view.php?id=1\n")
	_, err := runCLI(t, "--headless")
	if err == nil || !strings.Contains(err.Error(), "falta la cookie") {
		t.Fatalf("want missing-cookie error, got %v", err)
	}
	if DetermineExitCode(context.Background(), err) != ExitUsageErr {
		t.Errorf("missing cookie must be a usage error")
	}

	pipeStdin(t, "https://campusvirtual.ufro.cl/mod/resource/view.php?id=1\n")
	if _, err := runCLI(t, "--headless", "--uni", "nope"); !errors.Is(err, auth.ErrUnknownUniversity) {
		t.Fatalf("want ErrUnknownUniversity, got %v", err)
	}
}

func TestRootCmd_PipedDemoWritesLogAndFile(t *testing.T) {
	out := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "d.log")
	pipeStdin(t, "https://campusvirtual.ufro.cl/mod/resource/view.php?id=7\n")
	if _, err := runCLI(t, "--demo", "--headless", "-o", out, "-l", logPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("log file not written: %v", err)
	}
	if m, _ := filepath.Glob(filepath.Join(out, "*.pdf")); len(m) != 1 {
		t.Errorf("want 1 pdf, got %v", m)
	}
}

func TestRunCmd_InputSources(t *testing.T) {
	t.Setenv("MOODLE_SESSION", "")
	withAuthFlags(t, "", "", fakeCookies{})
	out := t.TempDir()

	// --file with blank lines, comma-separated --urls and demo mode.
	file := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(file, []byte("\nhttps://campusvirtual.ufro.cl/mod/resource/view.php?id=21\n  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "run", "--demo", "--headless", "-o", out, "-f", file,
		"-u", "https://campusvirtual.ufro.cl/mod/resource/view.php?id=22, ,"); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(filepath.Join(out, "*.pdf")); len(m) != 2 {
		t.Errorf("want 2 pdfs (file + urls), got %v", m)
	}

	// Unreadable file.
	if _, err := runCLI(t, "run", "-f", filepath.Join(t.TempDir(), "nope.txt")); err == nil || !strings.Contains(err.Error(), "archivo de URLs") {
		t.Errorf("want read error, got %v", err)
	}

	// Real (non-demo) URL but no cookie source at all.
	_, err := runCLI(t, "run", "--headless", "-u", "https://campusvirtual.ufro.cl/mod/resource/view.php?id=1")
	if err == nil || !strings.Contains(err.Error(), "se requiere cookie") {
		t.Errorf("want cookie-required error, got %v", err)
	}

	// Invalid domain surfaces as usage error before any download.
	_, err = runCLI(t, "run", "--headless", "--domain", "bad domain", "-u", "https://campusvirtual.ufro.cl/mod/resource/view.php?id=1")
	if !errors.Is(err, auth.ErrInvalidDomain) {
		t.Errorf("want ErrInvalidDomain, got %v", err)
	}

	// Stdin fallback when no flags provide URLs.
	pipeStdin(t, "https://campusvirtual.ufro.cl/mod/resource/view.php?id=31\n")
	out2 := t.TempDir()
	if _, err := runCLI(t, "run", "--demo", "--headless", "-o", out2); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(filepath.Join(out2, "*.pdf")); len(m) != 1 {
		t.Errorf("want 1 pdf from stdin, got %v", m)
	}
}
