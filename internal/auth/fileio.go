package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	// tempDirPrefix names the private folders of the copy fallback.
	tempDirPrefix = "moofetch-cookies-"
	// removeTries is how many times deleting the temporary folder is attempted.
	removeTries = 5
	// staleTempAge is how old a leftover temporary folder must be to be swept.
	staleTempAge = 10 * time.Minute
)

var (
	// errTempCleanup means the temporary copy of a browser file could not be
	// deleted, so sensitive data may remain in the temporary folder.
	errTempCleanup = errors.New("no se pudo borrar la carpeta temporal")
	// errTooBig means a file exceeds maxReadBytes.
	errTooBig = errors.New("archivo demasiado grande")
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
	// maxReadBytes bounds how much of a file is loaded into memory (64 MiB:
	// Firefox session backups decode to at most that much anyway).
	maxReadBytes int64 = 64 << 20
	// tempBase is the folder where temporary copies are made. On Windows the
	// 0700/0600 modes mean nothing: privacy relies on the per-user %TEMP% ACL.
	tempBase = os.TempDir
	// nowFn is the clock used to age leftover temporary folders.
	nowFn = time.Now
	// makeTempDir creates the private folder used by the copy fallback.
	makeTempDir = func() (string, error) { return os.MkdirTemp(tempBase(), tempDirPrefix) }
	// removeAll deletes the temporary folder.
	removeAll = os.RemoveAll
)

// readShared opens path with openFileShared and reads it entirely, up to
// maxReadBytes. The file size is used as a hint to avoid growing the buffer.
func readShared(path string) ([]byte, error) {
	f, err := openFileShared(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var buf bytes.Buffer
	if info, serr := f.Stat(); serr == nil {
		if info.Size() > maxReadBytes {
			return nil, fmt.Errorf("%w: %s", errTooBig, filepath.Base(path))
		}
		buf.Grow(int(info.Size()) + 1)
	}
	if _, err := buf.ReadFrom(io.LimitReader(f, maxReadBytes+1)); err != nil {
		return nil, err
	}
	if int64(buf.Len()) > maxReadBytes {
		return nil, fmt.Errorf("%w: %s", errTooBig, filepath.Base(path))
	}
	return buf.Bytes(), nil
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
// fails, the temporary copy. It returns the strategy that worked, or
// StrategyNone and an error. If the data was read from the copy but the copy
// could not be deleted, it returns the data, StrategyCopy AND the cleanup
// error (errors.Is(err, errTempCleanup)).
func readFileResilient(ctx context.Context, path string) ([]byte, Strategy, error) {
	data, err := readSharedRetry(ctx, path)
	if err == nil {
		return data, StrategyShared, nil
	}
	if !copyWorthTrying(ctx, err) {
		return nil, StrategyNone, err
	}
	var copied []byte
	read := false
	cerr := withTempCopy(ctx, path, func(p string) error {
		var rerr error
		copied, rerr = readShared(p)
		read = rerr == nil
		return rerr
	})
	if read {
		return copied, StrategyCopy, cerr
	}
	return nil, StrategyNone, fmt.Errorf("%w; copia temporal: %v", err, cerr)
}

// withTempCopy copies src into a private temporary folder, calls fn with the
// path of the copy and ALWAYS removes the folder afterwards, also on error,
// cancellation or panic. If the folder cannot be deleted the returned error
// wraps errTempCleanup, so the leftover shows up in reports.
//
// Only src is copied, not SQLite's "-wal"/"-shm" siblings: the sqlite reader
// used by kooky has no WAL support, so copying them would only leave more
// plaintext on disk for no benefit.
func withTempCopy(ctx context.Context, src string, fn func(copyPath string) error) (err error) {
	sweepStaleTempDirs(tempBase(), nowFn())
	dir, merr := makeTempDir()
	if merr != nil {
		return merr
	}
	defer func() {
		if rerr := removeDir(dir); rerr != nil {
			err = errors.Join(err, fmt.Errorf("%w: %v", errTempCleanup, rerr))
		}
	}()
	// The folder has mode 0700 (MkdirTemp) and the file 0600.

	dst := filepath.Join(dir, filepath.Base(src))
	if err := copyShared(ctx, src, dst); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(dst)
}

// copyShared copies src to dst (created 0600) reading src with a shared open.
// A file larger than maxReadBytes is an error, never a silent truncation.
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
	n, err := io.Copy(out, io.LimitReader(in, maxReadBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxReadBytes {
		err = fmt.Errorf("%w: %s", errTooBig, filepath.Base(src))
	}
	return err
}

// removeDir deletes dir, trying several times with growing pauses because
// antivirus software can keep a freshly written file open for a while on
// Windows. It returns the last error if the folder could not be deleted.
func removeDir(dir string) error {
	delay := 20 * time.Millisecond
	var err error
	for attempt := 0; attempt < removeTries; attempt++ {
		if err = removeAll(dir); err == nil {
			return nil
		}
		if attempt < removeTries-1 {
			_ = sleepCtx(context.Background(), delay)
			delay *= 2
		}
	}
	return err
}

// sweepStaleTempDirs removes leftover temporary copies older than
// staleTempAge from base (for example after a crash or a failed cleanup).
// Best effort: every error is ignored.
func sweepStaleTempDirs(base string, now time.Time) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), tempDirPrefix) {
			continue
		}
		if info, ierr := e.Info(); ierr == nil && now.Sub(info.ModTime()) > staleTempAge {
			_ = removeAll(filepath.Join(base, e.Name()))
		}
	}
}
