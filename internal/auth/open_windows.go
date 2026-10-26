//go:build windows

package auth

import (
	"os"
	"syscall"
)

// openShared opens path read-only allowing every other process to keep reading,
// writing and even renaming/deleting the file while we hold it. Browsers keep
// their cookie and session files open; os.Open does not set
// FILE_SHARE_DELETE, so it can fail with a sharing violation on files that the
// browser itself opened with that mode. The approach mirrors the one used by
// the kooky library (its internal package cannot be imported).
func openShared(path string) (*os.File, error) {
	if path == "" {
		return nil, &os.PathError{Op: "open", Path: path, Err: syscall.ERROR_FILE_NOT_FOUND}
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	const share = syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, share, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
