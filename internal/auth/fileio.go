package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Reading strategy for files MooFetch reads itself (Firefox session backups
// and cookies.sqlite of extra profiles):
//
//  1. Shared read: open allowing other processes to keep using the file,
//     read it into memory. A sharing/lock violation is retried a few times
//     because browsers hold their files only for an instant.
//  2. Fallback copy-read-delete: copy the file into a private temporary
//     folder, read the copy and always delete the folder. It leaves sensitive
//     data on disk for an instant and does NOT bypass an exclusive lock (the
//     copy itself needs to read the original), so it is a last resort.

const (
	// lockRetries is how many times a locked file is retried after the first attempt.
	lockRetries = 3
	// maxReadBytes bounds how much of a file is loaded into memory.
	maxReadBytes = 128 << 20
)

// Seams replaced by tests.
var (
	// lockRetryDelay is the pause before the first retry; it doubles each time.
	lockRetryDelay = 50 * time.Millisecond
	// openFileShared opens a file for a shared read.
	openFileShared = openShared
	// sleepCtx waits d or until ctx is done.
	sleepCtx = func(ctx context.Context, d time.Duration) error {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	}
	// makeTempDir creates the private folder used by the copy fallback.
	makeTempDir = func() (string, error) { return os.MkdirTemp("", "moofetch-cookies-") }
)

// readShared opens path with openFileShared and reads it entirely.
func readShared(path string) ([]byte, error) {
	f, err := openFileShared(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxReadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReadBytes {
		return nil, fmt.Errorf("archivo demasiado grande: %s", filepath.Base(path))
	}
	return data, nil
}

// retryOnLock runs op, retrying up to lockRetries times with growing pauses
// while it fails with a sharing/lock violation. Other errors return at once.
func retryOnLock(ctx context.Context, op func() error) error {
	delay := lockRetryDelay
	var err error
	for attempt := 0; ; attempt++ {
		if err = op(); err == nil || !isLockError(err) || attempt >= lockRetries {
			return err
		}
		if cerr := sleepCtx(ctx, delay); cerr != nil {
			return cerr
		}
		delay *= 2
	}
}

// readSharedRetry is the shared read of one file with lock retries.
func readSharedRetry(ctx context.Context, path string) ([]byte, error) {
	var data []byte
	err := retryOnLock(ctx, func() error {
		var rerr error
		data, rerr = readShared(path)
		return rerr
	})
	return data, err
}

// copyWorthTrying reports whether a failed shared read justifies the copy
// fallback: not for a missing file and not once the context is done.
func copyWorthTrying(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() == nil && !errors.Is(err, fs.ErrNotExist)
}

// readFileResilient reads a whole file using the shared read and, if that
// fails, the temporary copy. It returns the strategy that worked.
func readFileResilient(ctx context.Context, path string) ([]byte, Strategy, error) {
	data, err := readSharedRetry(ctx, path)
	if err == nil {
		return data, StrategyShared, nil
	}
	if !copyWorthTrying(ctx, err) {
		return nil, StrategyNone, err
	}
	var copied []byte
	cerr := withTempCopy(ctx, path, nil, func(p string) error {
		var rerr error
		copied, rerr = readShared(p)
		return rerr
	})
	if cerr != nil {
		return nil, StrategyNone, fmt.Errorf("%w; copia temporal: %v", err, cerr)
	}
	return copied, StrategyCopy, nil
}

// withTempCopy copies src (and any existing src+suffix siblings, such as the
// SQLite "-wal" and "-shm" files) into a private temporary folder, calls fn
// with the path of the copy and ALWAYS removes the folder afterwards, also on
// error or cancellation.
func withTempCopy(ctx context.Context, src string, siblingSuffixes []string, fn func(copyPath string) error) error {
	dir, err := makeTempDir()
	if err != nil {
		return err
	}
	defer removeAllRetry(dir)
	_ = os.Chmod(dir, 0o700) // MkdirTemp already uses 0700; be explicit.

	dst := filepath.Join(dir, filepath.Base(src))
	if err := copyShared(ctx, src, dst); err != nil {
		return err
	}
	for _, suffix := range siblingSuffixes {
		// Best effort: a missing or unreadable -wal/-shm only means a slightly older snapshot.
		_ = copyShared(ctx, src+suffix, dst+suffix)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(dst)
}

// copyShared copies src to dst (created 0600) reading src with a shared open.
func copyShared(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	in, err := openFileShared(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(in, maxReadBytes)); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// removeAllRetry deletes dir, trying a few times because antivirus software
// can briefly keep a freshly written file open on Windows.
func removeAllRetry(dir string) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := os.RemoveAll(dir); err == nil {
			return
		}
		_ = sleepCtx(context.Background(), 20*time.Millisecond)
	}
}
