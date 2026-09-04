package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"godownloader/internal/kernel"
	"godownloader/internal/plugins/moodle"
	"godownloader/internal/tui"
)

func TestDetermineExitCode_Success(t *testing.T) {
	code := DetermineExitCode(nil, context.Background())
	if code != ExitSuccess {
		t.Errorf("expected ExitSuccess (%d), got %d", ExitSuccess, code)
	}
}

func TestDetermineExitCode_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	code := DetermineExitCode(context.Canceled, ctx)
	if code != ExitInterrupted {
		t.Errorf("expected ExitInterrupted (%d), got %d", ExitInterrupted, code)
	}
}

func TestDetermineExitCode_FormAborted(t *testing.T) {
	code := DetermineExitCode(tui.ErrFormAborted, context.Background())
	if code != ExitInterrupted {
		t.Errorf("expected ExitInterrupted (%d) on ErrFormAborted, got %d", ExitInterrupted, code)
	}
}

func TestDetermineExitCode_AuthErrorDirect(t *testing.T) {
	code := DetermineExitCode(moodle.ErrAuthenticationFailed, context.Background())
	if code != ExitAuthErr {
		t.Errorf("expected ExitAuthErr (%d), got %d", ExitAuthErr, code)
	}
}

func TestDetermineExitCode_AuthErrorInBatch(t *testing.T) {
	batchErr := &BatchError{
		Results: []kernel.Result{
			{TaskID: 1, URL: "https://example.com/1", Err: moodle.ErrAuthenticationFailed},
		},
	}

	code := DetermineExitCode(batchErr, context.Background())
	if code != ExitAuthErr {
		t.Errorf("expected ExitAuthErr (%d) for batch auth error, got %d", ExitAuthErr, code)
	}
}

func TestDetermineExitCode_CancelInBatch(t *testing.T) {
	batchErr := &BatchError{
		Results: []kernel.Result{
			{TaskID: 1, URL: "https://example.com/1", Err: context.Canceled},
		},
	}

	code := DetermineExitCode(batchErr, context.Background())
	if code != ExitInterrupted {
		t.Errorf("expected ExitInterrupted (%d) for canceled batch, got %d", ExitInterrupted, code)
	}
}

func TestDetermineExitCode_UsageError(t *testing.T) {
	tests := []error{
		errors.New("unknown flag: --invalid"),
		errors.New("se requiere cookie de sesión (usa --cookie (-k) o la variable MOODLE_SESSION)"),
		errors.New("no se especificaron URLs válidas para descargar"),
	}

	for _, err := range tests {
		code := DetermineExitCode(err, context.Background())
		if code != ExitUsageErr {
			t.Errorf("expected ExitUsageErr (%d) for %q, got %d", ExitUsageErr, err, code)
		}
	}
}

func TestDetermineExitCode_GeneralError(t *testing.T) {
	generalErr := errors.New("i/o timeout")
	code := DetermineExitCode(generalErr, context.Background())
	if code != ExitGeneralErr {
		t.Errorf("expected ExitGeneralErr (%d), got %d", ExitGeneralErr, code)
	}
}

func TestExitCode_AuthFailureEndToEnd(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login/index.php", http.StatusSeeOther)
	}))
	defer ts.Close()

	rootCmd.SetArgs([]string{
		"run",
		"--cookie", "expired_token",
		"--urls", ts.URL + "/mod/resource/view.php?id=123",
		"--output", t.TempDir(),
		"--headless",
	})

	err := Execute(context.Background())
	if err == nil {
		t.Fatalf("expected error for unauthenticated download, got nil")
	}

	exitCode := DetermineExitCode(err, context.Background())
	if exitCode != ExitAuthErr {
		t.Errorf("expected exit code %d (ExitAuthErr), got %d (err: %v)", ExitAuthErr, exitCode, err)
	}
}

