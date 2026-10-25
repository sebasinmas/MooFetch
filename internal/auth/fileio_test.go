package auth

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/browserutils/kooky"
)

var errBusy = errors.New("The process cannot access the file because it is being used by another process.")

// failOpener makes the first n opens fail with errBusy and then behaves like
// os.Open. It returns the number of calls made.
func failOpener(t *testing.T, n int32) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	old := openFileShared
	openFileShared = func(path string) (*os.File, error) {
		if calls.Add(1) <= n {
			return nil, &fs.PathError{Op: "open", Path: path, Err: errBusy}
		}
		return os.Open(path)
	}
	t.Cleanup(func() { openFileShared = old })
	return &calls
}

// trackTempDirs records every folder created by the copy fallback.
func trackTempDirs(t *testing.T) *[]string {
	t.Helper()
	var dirs []string
	old := makeTempDir
	makeTempDir = func() (string, error) {
		d, err := old()
		if err == nil {
			dirs = append(dirs, d)
		}
		return d, err
	}
	t.Cleanup(func() { makeTempDir = old })
	return &dirs
}

func assertGone(t *testing.T, dirs *[]string) {
	t.Helper()
	if len(*dirs) == 0 {
		t.Fatal("expected the copy fallback to create a temporary folder")
	}
	for _, d := range *dirs {
		if _, err := os.Stat(d); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("temporary folder %s was not removed (stat err: %v)", d, err)
		}
	}
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRetryOnLock_RetriesThenSucceeds(t *testing.T) {
	sleeps := noSleep(t)
	calls := 0
	err := retryOnLock(context.Background(), func() error {
		calls++
		if calls < 3 {
			return errBusy
		}
		return nil
	})
	if err != nil || calls != 3 || sleeps.Load() != 2 {
		t.Fatalf("err=%v calls=%d sleeps=%d", err, calls, sleeps.Load())
	}
}

func TestRetryOnLock_IsBounded(t *testing.T) {
	sleeps := noSleep(t)
	calls := 0
	err := retryOnLock(context.Background(), func() error { calls++; return errBusy })
	if !errors.Is(err, errBusy) || calls != lockRetries+1 || int(sleeps.Load()) != lockRetries {
		t.Fatalf("err=%v calls=%d sleeps=%d", err, calls, sleeps.Load())
	}
}

func TestRetryOnLock_OtherErrorsAreNotRetried(t *testing.T) {
	sleeps := noSleep(t)
	calls := 0
	boom := errors.New("boom")
	if err := retryOnLock(context.Background(), func() error { calls++; return boom }); !errors.Is(err, boom) || calls != 1 || sleeps.Load() != 0 {
		t.Fatalf("err=%v calls=%d sleeps=%d", err, calls, sleeps.Load())
	}
}

func TestRetryOnLock_StopsWhenContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := retryOnLock(ctx, func() error { calls++; return errBusy })
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestSleepCtx(t *testing.T) {
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestReadFileResilient_SharedReadAfterRetries(t *testing.T) {
	noSleep(t)
	calls := failOpener(t, 2)
	p := writeTemp(t, "recovery.jsonlz4", "hello")
	data, strategy, err := readFileResilient(context.Background(), p)
	if err != nil || string(data) != "hello" || strategy != StrategyShared || calls.Load() != 3 {
		t.Fatalf("data=%q strategy=%s err=%v calls=%d", data, strategy, err, calls.Load())
	}
}

func TestReadFileResilient_FallsBackToTemporaryCopy(t *testing.T) {
	noSleep(t)
	dirs := trackTempDirs(t)
	// The first 1+lockRetries opens (the shared read and its retries) fail; the copy works.
	failOpener(t, lockRetries+1)
	p := writeTemp(t, "recovery.jsonlz4", "from copy")
	data, strategy, err := readFileResilient(context.Background(), p)
	if err != nil || string(data) != "from copy" || strategy != StrategyCopy {
		t.Fatalf("data=%q strategy=%s err=%v", data, strategy, err)
	}
	assertGone(t, dirs)
}

func TestReadFileResilient_BothStrategiesFail(t *testing.T) {
	noSleep(t)
	dirs := trackTempDirs(t)
	failOpener(t, 1000)
	p := writeTemp(t, "recovery.jsonlz4", "x")
	_, strategy, err := readFileResilient(context.Background(), p)
	if err == nil || strategy != StrategyNone || classifyError(err) != OutcomeLocked {
		t.Fatalf("strategy=%s err=%v outcome=%s", strategy, err, classifyError(err))
	}
	assertGone(t, dirs)
}

func TestReadFileResilient_MissingFileSkipsCopy(t *testing.T) {
	dirs := trackTempDirs(t)
	_, strategy, err := readFileResilient(context.Background(), filepath.Join(t.TempDir(), "nope"))
	if !errors.Is(err, fs.ErrNotExist) || strategy != StrategyNone || len(*dirs) != 0 {
		t.Fatalf("strategy=%s err=%v dirs=%v", strategy, err, *dirs)
	}
}

func TestReadShared_ReadsWholeFile(t *testing.T) {
	p := writeTemp(t, "big", "0123456789")
	old := openFileShared
	openFileShared = func(path string) (*os.File, error) { return os.Open(path) }
	t.Cleanup(func() { openFileShared = old })
	if data, err := readShared(p); err != nil || len(data) != 10 {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func TestWithTempCopy_CopiesSiblingsAndCleansUp(t *testing.T) {
	dirs := trackTempDirs(t)
	src := writeTemp(t, "cookies.sqlite", "db")
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(src+suffix, []byte("side"+suffix), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	err := withTempCopy(context.Background(), src, []string{"-wal", "-shm", "-missing"}, func(copyPath string) error {
		for _, s := range []string{"", "-wal", "-shm", "-missing"} {
			if _, err := os.Stat(copyPath + s); err == nil {
				seen = append(seen, s)
			}
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(filepath.Dir(copyPath))
			if err != nil || info.Mode().Perm() != 0o700 {
				t.Errorf("temporary folder must be private (0700): %v %v", info, err)
			}
		}
		return nil
	})
	if err != nil || len(seen) != 3 {
		t.Fatalf("err=%v copied=%v", err, seen)
	}
	assertGone(t, dirs)
}

func TestWithTempCopy_RemovesFolderWhenReadFails(t *testing.T) {
	dirs := trackTempDirs(t)
	src := writeTemp(t, "cookies.sqlite", "db")
	boom := errors.New("read failed")
	if err := withTempCopy(context.Background(), src, nil, func(string) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	assertGone(t, dirs)
}

func TestWithTempCopy_RemovesFolderWhenContextIsCancelled(t *testing.T) {
	dirs := trackTempDirs(t)
	src := writeTemp(t, "cookies.sqlite", "db")
	ctx, cancel := context.WithCancel(context.Background())
	called := false
	err := withTempCopy(ctx, src, nil, func(string) error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("sanity: %v %v", err, called)
	}
	assertGone(t, dirs)

	// Cancelled while the folder exists: the read callback cancels, and the
	// following context check (and the copy of an already-cancelled context) must still clean up.
	*dirs = nil
	err = withTempCopy(ctx, src, nil, func(string) error { cancel(); return ctx.Err() })
	if err == nil {
		t.Fatal("expected an error")
	}
	assertGone(t, dirs)

	*dirs = nil
	if err := withTempCopy(ctx, src, nil, func(string) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled context must stop the copy, got %v", err)
	}
	assertGone(t, dirs)
}

func TestWithTempCopy_SourceMissingStillCleansUp(t *testing.T) {
	dirs := trackTempDirs(t)
	err := withTempCopy(context.Background(), filepath.Join(t.TempDir(), "nope"), nil, func(string) error { return nil })
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
	assertGone(t, dirs)
}

func TestWithTempCopy_TempDirFailure(t *testing.T) {
	old := makeTempDir
	makeTempDir = func() (string, error) { return "", errors.New("no space") }
	t.Cleanup(func() { makeTempDir = old })
	if err := withTempCopy(context.Background(), "x", nil, func(string) error { return nil }); err == nil {
		t.Fatal("expected an error")
	}
}

func TestReadSQLiteResilient(t *testing.T) {
	noSleep(t)
	p := writeTemp(t, "cookies.sqlite", "db")
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(p+suffix, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldRead := readSQLite
	t.Cleanup(func() { readSQLite = oldRead })
	cookie := ck("v", time.Now(), time.Time{})
	all := kooky.FilterFunc(func(*kooky.Cookie) bool { return true })

	t.Run("shared", func(t *testing.T) {
		readSQLite = func(string, kooky.Filter) ([]*kooky.Cookie, error) { return []*kooky.Cookie{cookie}, nil }
		cs, strategy, err := readSQLiteResilient(context.Background(), p, all)
		if err != nil || len(cs) != 1 || strategy != StrategyShared {
			t.Fatalf("%v %s %v", cs, strategy, err)
		}
	})
	t.Run("retries a locked file", func(t *testing.T) {
		calls := 0
		readSQLite = func(string, kooky.Filter) ([]*kooky.Cookie, error) {
			calls++
			if calls < 3 {
				return nil, errBusy
			}
			return []*kooky.Cookie{cookie}, nil
		}
		_, strategy, err := readSQLiteResilient(context.Background(), p, all)
		if err != nil || strategy != StrategyShared || calls != 3 {
			t.Fatalf("%s %v calls=%d", strategy, err, calls)
		}
	})
	t.Run("copy with wal and shm", func(t *testing.T) {
		dirs := trackTempDirs(t)
		readSQLite = func(path string, _ kooky.Filter) ([]*kooky.Cookie, error) {
			if path == p {
				return nil, errBusy
			}
			for _, s := range []string{"-wal", "-shm"} {
				if _, err := os.Stat(path + s); err != nil {
					t.Errorf("sibling %s not copied: %v", s, err)
				}
			}
			return []*kooky.Cookie{cookie}, nil
		}
		cs, strategy, err := readSQLiteResilient(context.Background(), p, all)
		if err != nil || len(cs) != 1 || strategy != StrategyCopy {
			t.Fatalf("%v %s %v", cs, strategy, err)
		}
		assertGone(t, dirs)
	})
	t.Run("both fail", func(t *testing.T) {
		dirs := trackTempDirs(t)
		readSQLite = func(string, kooky.Filter) ([]*kooky.Cookie, error) { return nil, errBusy }
		_, strategy, err := readSQLiteResilient(context.Background(), p, all)
		if err == nil || strategy != StrategyNone || classifyError(err) != OutcomeLocked {
			t.Fatalf("%s %v", strategy, err)
		}
		assertGone(t, dirs)
	})
	t.Run("missing file skips the copy", func(t *testing.T) {
		dirs := trackTempDirs(t)
		readSQLite = func(string, kooky.Filter) ([]*kooky.Cookie, error) { return nil, fs.ErrNotExist }
		if _, _, err := readSQLiteResilient(context.Background(), p, all); !errors.Is(err, fs.ErrNotExist) || len(*dirs) != 0 {
			t.Fatalf("%v %v", err, *dirs)
		}
	})
}
