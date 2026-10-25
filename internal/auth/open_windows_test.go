//go:build windows

package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// holdOpen keeps path open with the given share mode until the test ends,
// like a browser holding its cookie file.
func holdOpen(t *testing.T, path string, share uint32) {
	t.Helper()
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, share, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("could not hold the file open: %v", err)
	}
	t.Cleanup(func() { _ = syscall.CloseHandle(h) })
}

func newFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Cookies")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpenShared_ReadsFileHeldWithFullSharing(t *testing.T) {
	p := newFile(t, "session-data")
	holdOpen(t, p, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE)

	data, err := readShared(p)
	if err != nil || string(data) != "session-data" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	got, strategy, err := readFileResilient(context.Background(), p)
	if err != nil || string(got) != "session-data" || strategy != StrategyShared {
		t.Fatalf("data=%q strategy=%s err=%v", got, strategy, err)
	}
}

func TestOpenShared_ExclusiveLockFailsFastAndIsClassifiedAsLocked(t *testing.T) {
	p := newFile(t, "session-data")
	holdOpen(t, p, 0) // share mode 0: nobody else may open it

	if _, err := openShared(p); err == nil {
		t.Fatal("opening an exclusively locked file must fail")
	} else if !isLockError(err) || classifyError(err) != OutcomeLocked {
		t.Fatalf("want a lock error classified as %s, got %v (%s)", OutcomeLocked, err, classifyError(err))
	}

	// No hang: retries are bounded (we only count the pauses, we do not wait).
	var sleeps int
	old := sleepCtx
	sleepCtx = func(context.Context, time.Duration) error { sleeps++; return nil }
	t.Cleanup(func() { sleepCtx = old })

	start := time.Now()
	_, strategy, err := readFileResilient(context.Background(), p)
	if time.Since(start) > 5*time.Second {
		t.Fatal("reading a locked file must not hang")
	}
	if err == nil || strategy != StrategyNone || classifyError(err) != OutcomeLocked {
		t.Fatalf("strategy=%s err=%v outcome=%s", strategy, err, classifyError(err))
	}
	// 1 shared read with lockRetries pauses; the copy fallback also fails on the first open.
	if sleeps != lockRetries {
		t.Fatalf("pauses = %d, want exactly %d", sleeps, lockRetries)
	}
}

func TestOpenShared_MissingFileIsNotExist(t *testing.T) {
	_, err := openShared(filepath.Join(t.TempDir(), "nope"))
	if !errors.Is(err, os.ErrNotExist) || classifyError(err) != OutcomeMissing {
		t.Fatalf("got %v (%s)", err, classifyError(err))
	}
	if _, err := openShared(""); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty path: got %v", err)
	}
}

func TestIsOSLockError_Windows(t *testing.T) {
	for _, tc := range []struct {
		errno syscall.Errno
		want  bool
	}{
		{32, true}, {33, true}, {5, false}, {2, false},
	} {
		err := &os.PathError{Op: "open", Path: "x", Err: tc.errno}
		if got := isOSLockError(err); got != tc.want {
			t.Errorf("errno %d: got %v want %v", tc.errno, got, tc.want)
		}
	}
	if isOSLockError(errors.New("plain")) {
		t.Error("a plain error is not an OS lock error")
	}
}
