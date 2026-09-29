package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sebasinmas/MooFetch/internal/kernel"
	"github.com/sebasinmas/MooFetch/internal/plugins/moodle"
	"github.com/sebasinmas/MooFetch/internal/tui"
)

// customFatalAuthMock implements kernel.FatalAuthError interface for polymorphic contract testing.
type customFatalAuthMock struct {
	message string
	isFatal bool
}

func (c customFatalAuthMock) Error() string {
	return c.message
}

func (c customFatalAuthMock) IsFatalAuth() bool {
	return c.isFatal
}

// TestDetermineExitCode_POSIXMappingTDT executes a comprehensive Table-Driven Test suite
// verifying that every expected error type, context state, and batch result maps strictly
// to its contracted POSIX exit code specification.
func TestDetermineExitCode_POSIXMappingTDT(t *testing.T) {
	t.Parallel()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name         string
		err          error
		ctx          context.Context
		expectedCode int
	}{
		// ---------------------------------------------------------------------
		// POSIX 0: ExitSuccess
		// ---------------------------------------------------------------------
		{
			name:         "Success: nil error with active background context",
			err:          nil,
			ctx:          context.Background(),
			expectedCode: ExitSuccess,
		},
		{
			name:         "Success: nil error with nil context",
			err:          nil,
			ctx:          nil,
			expectedCode: ExitSuccess,
		},

		// ---------------------------------------------------------------------
		// POSIX 130: ExitInterrupted (128 + SIGINT/SIGTERM or cancelled context)
		// ---------------------------------------------------------------------
		{
			name:         "Interrupted: context.Canceled directly",
			err:          context.Canceled,
			ctx:          context.Background(),
			expectedCode: ExitInterrupted,
		},
		{
			name:         "Interrupted: wrapped context.Canceled error",
			err:          fmt.Errorf("operation terminated by signal: %w", context.Canceled),
			ctx:          context.Background(),
			expectedCode: ExitInterrupted,
		},
		{
			name:         "Interrupted: tui.ErrFormAborted when user cancels interactive wizard",
			err:          tui.ErrFormAborted,
			ctx:          context.Background(),
			expectedCode: ExitInterrupted,
		},
		{
			name:         "Interrupted: context with error already canceled",
			err:          errors.New("arbitrary network error during teardown"),
			ctx:          canceledCtx,
			expectedCode: ExitInterrupted,
		},
		{
			name: "Interrupted: BatchError where all tasks were canceled",
			err: &BatchError{
				Results: []kernel.Result{
					{TaskID: 1, URL: "https://example.com/1", Err: context.Canceled},
					{TaskID: 2, URL: "https://example.com/2", Err: context.Canceled},
				},
			},
			ctx:          context.Background(),
			expectedCode: ExitInterrupted,
		},

		// ---------------------------------------------------------------------
		// POSIX 77: ExitAuthErr (EX_NOPERM - Authentication failure)
		// ---------------------------------------------------------------------
		{
			name:         "Auth: moodle.ErrAuthenticationFailed directly",
			err:          moodle.ErrAuthenticationFailed,
			ctx:          context.Background(),
			expectedCode: ExitAuthErr,
		},
		{
			name:         "Auth: kernel.ErrAuthenticationFailed directly",
			err:          kernel.ErrAuthenticationFailed,
			ctx:          context.Background(),
			expectedCode: ExitAuthErr,
		},
		{
			name:         "Auth: wrapped error containing kernel.ErrAuthenticationFailed",
			err:          fmt.Errorf("moodle session expired: %w", kernel.ErrAuthenticationFailed),
			ctx:          context.Background(),
			expectedCode: ExitAuthErr,
		},
		{
			name:         "Auth: custom struct implementing kernel.FatalAuthError interface",
			err:          customFatalAuthMock{message: "SSO token expired", isFatal: true},
			ctx:          context.Background(),
			expectedCode: ExitAuthErr,
		},
		{
			name:         "Auth: text containing 'authentication failed' (case-insensitive)",
			err:          errors.New("HTTP 403: Authentication Failed due to bad session"),
			ctx:          context.Background(),
			expectedCode: ExitAuthErr,
		},
		{
			name: "Auth: BatchError where all failed tasks have authentication failures",
			err: &BatchError{
				Results: []kernel.Result{
					{TaskID: 1, URL: "https://example.com/1", Err: moodle.ErrAuthenticationFailed},
					{TaskID: 2, URL: "https://example.com/2", Err: kernel.ErrAuthenticationFailed},
				},
			},
			ctx:          context.Background(),
			expectedCode: ExitAuthErr,
		},

		// ---------------------------------------------------------------------
		// POSIX 2: ExitUsageErr (Invalid flags, parameters, or missing inputs)
		// ---------------------------------------------------------------------
		{
			name:         "Usage: unknown flag",
			err:          errors.New("unknown flag: --concurreny"),
			ctx:          context.Background(),
			expectedCode: ExitUsageErr,
		},
		{
			name:         "Usage: unknown shorthand flag",
			err:          errors.New("unknown shorthand flag: 'z' in -z"),
			ctx:          context.Background(),
			expectedCode: ExitUsageErr,
		},
		{
			name:         "Usage: flag needs an argument",
			err:          errors.New("flag needs an argument: --output"),
			ctx:          context.Background(),
			expectedCode: ExitUsageErr,
		},
		{
			name:         "Usage: required flag missing",
			err:          errors.New("required flag(s) \"cookie\" not set"),
			ctx:          context.Background(),
			expectedCode: ExitUsageErr,
		},
		{
			name:         "Usage: no URLs specified via flags or pipes",
			err:          errors.New("no se especificaron urls válidas para descargar"),
			ctx:          context.Background(),
			expectedCode: ExitUsageErr,
		},
		{
			name:         "Usage: session cookie missing in non-demo run",
			err:          errors.New("se requiere cookie de sesión (usa --cookie (-k) o la variable MOODLE_SESSION)"),
			ctx:          context.Background(),
			expectedCode: ExitUsageErr,
		},
		{
			name:         "Usage: empty or malformed URLs from stdin pipe",
			err:          errors.New("no se detectaron URLs válidas desde la entrada estándar (stdin)"),
			ctx:          context.Background(),
			expectedCode: ExitUsageErr,
		},

		// ---------------------------------------------------------------------
		// POSIX 1: ExitGeneralErr (Runtime errors, disk I/O, network failures, mixed batches)
		// ---------------------------------------------------------------------
		{
			name:         "General: network I/O timeout",
			err:          errors.New("i/o timeout"),
			ctx:          context.Background(),
			expectedCode: ExitGeneralErr,
		},
		{
			name:         "General: server 500 error",
			err:          errors.New("unexpected HTTP response status: status 500"),
			ctx:          context.Background(),
			expectedCode: ExitGeneralErr,
		},
		{
			name:         "General: filesystem disk full error",
			err:          errors.New("failed writing to file: no space left on device"),
			ctx:          context.Background(),
			expectedCode: ExitGeneralErr,
		},
		{
			name: "General: BatchError with mixed failures (1 auth error + 1 network error)",
			err: &BatchError{
				Results: []kernel.Result{
					{TaskID: 1, URL: "https://example.com/1", Err: moodle.ErrAuthenticationFailed},
					{TaskID: 2, URL: "https://example.com/2", Err: errors.New("connection reset by peer")},
				},
			},
			ctx:          context.Background(),
			expectedCode: ExitGeneralErr,
		},
		{
			name: "General: BatchError with mixed failures (1 cancel + 1 disk error)",
			err: &BatchError{
				Results: []kernel.Result{
					{TaskID: 1, URL: "https://example.com/1", Err: context.Canceled},
					{TaskID: 2, URL: "https://example.com/2", Err: errors.New("write error")},
				},
			},
			ctx:          context.Background(),
			expectedCode: ExitGeneralErr,
		},
		{
			name: "General: BatchError with empty results",
			err: &BatchError{
				Results: []kernel.Result{},
			},
			ctx:          context.Background(),
			expectedCode: ExitGeneralErr,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			code := DetermineExitCode(tc.ctx, tc.err)
			if code != tc.expectedCode {
				t.Errorf("DetermineExitCode(%v) = %d; expected POSIX code %d", tc.err, code, tc.expectedCode)
			}
		})
	}
}

// TestBatchError_FormattingTDT validates that BatchError.Error() formats diagnostic messages
// cleanly for CLI consumers and GUI standard error parsers.
func TestBatchError_FormattingTDT(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		batch       *BatchError
		expectedStr string
	}{
		{
			name:        "Empty results slice",
			batch:       &BatchError{Results: []kernel.Result{}},
			expectedStr: "batch completed with errors",
		},
		{
			name: "Results with only successful (nil error) tasks",
			batch: &BatchError{
				Results: []kernel.Result{
					{TaskID: 1, Filename: "file1.pdf", Err: nil},
					{TaskID: 2, Filename: "file2.pdf", Err: nil},
				},
			},
			expectedStr: "batch completed with errors",
		},
		{
			name: "Single failed task",
			batch: &BatchError{
				Results: []kernel.Result{
					{TaskID: 1, Err: errors.New("forbidden: 403")},
				},
			},
			expectedStr: "forbidden: 403",
		},
		{
			name: "Multiple failed tasks joined by semicolon",
			batch: &BatchError{
				Results: []kernel.Result{
					{TaskID: 1, Err: errors.New("forbidden: 403")},
					{TaskID: 2, Err: errors.New("timeout after 30s")},
				},
			},
			expectedStr: "forbidden: 403; timeout after 30s",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			errStr := tc.batch.Error()
			if errStr != tc.expectedStr {
				t.Errorf("expected BatchError.Error() to be %q, got %q", tc.expectedStr, errStr)
			}
		})
	}
}

// TestDetermineExitCode_SimulatedEndToEnd_Parallel simulates a real unauthenticated batch
// against an httptest.Server returning HTTP 303 Redirect to login, verifying that the kernel
// dispatch result maps cleanly to ExitAuthErr (77) without global state contamination.
func TestDetermineExitCode_SimulatedEndToEnd_Parallel(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login/index.php", http.StatusSeeOther)
	}))
	defer ts.Close()

	plugin := moodle.New()
	k := kernel.New(kernel.WithPlugins([]kernel.DownloaderPlugin{plugin}))

	results := k.Dispatch(context.Background(), []kernel.Task{
		{ID: 1, URL: ts.URL + "/resource.pdf", Cookie: "expired_token", OutputDir: t.TempDir()},
	}, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Fatalf("expected task error due to login redirect, got nil")
	}

	batchErr := &BatchError{Results: results}
	code := DetermineExitCode(context.Background(), batchErr)
	if code != ExitAuthErr {
		t.Errorf("expected ExitAuthErr (%d), got %d", ExitAuthErr, code)
	}
}

// TestDetermineExitCode_RealCancellationSimulation verifies that when a context is cancelled
// during active task execution, DetermineExitCode strictly produces ExitInterrupted (130).
func TestDetermineExitCode_RealCancellationSimulation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // simulate Ctrl+C / SIGTERM

	code := DetermineExitCode(ctx, context.Canceled)
	if code != ExitInterrupted {
		t.Errorf("expected ExitInterrupted (%d), got %d", ExitInterrupted, code)
	}
}

// TestExitCode_AuthFailureEndToEnd verifies the Cobra CLI execution path maps an auth failure to ExitAuthErr.
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

	exitCode := DetermineExitCode(context.Background(), err)
	if exitCode != ExitAuthErr {
		t.Errorf("expected exit code %d (ExitAuthErr), got %d (err: %v)", ExitAuthErr, exitCode, err)
	}
}
