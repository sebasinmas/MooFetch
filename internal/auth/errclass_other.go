//go:build !windows

package auth

// isOSLockError has no native equivalent outside Windows (Unix does not lock
// files against readers); only the message-based check in isLockError applies.
func isOSLockError(error) bool { return false }
