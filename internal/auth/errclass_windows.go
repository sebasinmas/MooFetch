//go:build windows

package auth

import (
	"errors"
	"syscall"
)

const (
	errSharingViolation = syscall.Errno(32) // ERROR_SHARING_VIOLATION
	errLockViolation    = syscall.Errno(33) // ERROR_LOCK_VIOLATION
)

// isOSLockError reports the Windows errors returned when another process has
// the file open without sharing it.
func isOSLockError(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == errSharingViolation || errno == errLockViolation
	}
	return false
}
