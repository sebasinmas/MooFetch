package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sebasinmas/MooFetch/internal/plugins/moodle"
)

func TestRunScan_WritesOnlyURLsToStdout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "MoodleSession=abc" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`<html><body>
<a href="/mod/resource/view.php?id=7"><span class="instancename">Clase 1</span></a>
<a href="/mod/resource/view.php?id=7">dup</a>
<a href="/course/view.php?id=1">no es recurso</a>
</body></html>`))
	}))
	defer srv.Close()

	var out, diag bytes.Buffer
	opts := moodle.ScanOptions{Delay: -1, Jitter: -1}
	err := runScan(context.Background(), moodle.New(), srv.URL+"/course/view.php?id=1", "MoodleSession=abc", opts, &out, &diag)
	if err != nil {
		t.Fatalf("runScan: %v", err)
	}
	want := srv.URL + "/mod/resource/view.php?id=7\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if !strings.Contains(diag.String(), "1 recursos encontrados") || strings.Contains(out.String(), "Clase 1") {
		t.Errorf("summary/titles must go to stderr only: stdout=%q diag=%q", out.String(), diag.String())
	}
}

func TestRunScan_AuthFailureMapsToAuthExitCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	var out, diag bytes.Buffer
	err := runScan(context.Background(), moodle.New(), srv.URL+"/course/view.php?id=1", "MoodleSession=bad",
		moodle.ScanOptions{Delay: -1, Jitter: -1}, &out, &diag)
	if err == nil || !errors.Is(err, moodle.ErrAuthenticationFailed) {
		t.Fatalf("want auth error, got %v", err)
	}
	if got := DetermineExitCode(context.Background(), err); got != ExitAuthErr {
		t.Errorf("exit code = %d, want %d", got, ExitAuthErr)
	}
	if out.Len() != 0 {
		t.Errorf("stdout must stay empty on failure, got %q", out.String())
	}
}

func TestScanCmd_RequiresOneArgAndIsListed(t *testing.T) {
	if err := scanCmd.Args(scanCmd, nil); err == nil {
		t.Error("scan without URL should fail")
	}
	if rootCmd.Commands() == nil {
		t.Fatal("no commands")
	}
	found := false
	for _, c := range rootCmd.Commands() {
		found = found || c.Name() == "scan"
	}
	if !found {
		t.Error("scan not registered on root")
	}
}
