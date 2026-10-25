//go:build !windows

package auth

import "os"

// openShared opens path read-only. Outside Windows files are never locked
// against readers, so a plain os.Open is already a "shared" read.
func openShared(path string) (*os.File, error) {
	return os.Open(path)
}
