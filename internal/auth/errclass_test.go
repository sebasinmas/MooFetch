package auth

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Outcome
	}{
		{"nil", nil, OutcomeNoCookie},
		{"not exist", &fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist}, OutcomeMissing},
		{"wrapped not exist", fmt.Errorf("cookie store: %w", fs.ErrNotExist), OutcomeMissing},
		{"english windows message", errors.New("open Cookies: The process cannot access the file because it is being used by another process."), OutcomeLocked},
		{"spanish windows message", errors.New("El proceso no tiene acceso al archivo porque está siendo utilizado por otro proceso."), OutcomeLocked},
		{"sharing violation text", errors.New("open x: Sharing violation"), OutcomeLocked},
		{"lock violation text", errors.New("read x: The process cannot access the file because another process has locked a portion of the file. lock violation"), OutcomeLocked},
		{"sqlite locked", errors.New("database is locked"), OutcomeLocked},
		{"v20", errors.New("decrypting cookie: unsupported v20 App-Bound Encryption (requires elevated COM access)"), OutcomeEncrypted},
		{"app-bound only", errors.New("App-Bound encryption"), OutcomeEncrypted},
		{"encrypted wins over lock text", errors.New("v20 database is locked"), OutcomeEncrypted},
		{"timeout", errStoreTimeout, OutcomeUnreadable},
		{"anything else", errors.New("malformed database"), OutcomeUnreadable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyError(tc.err); got != tc.want {
				t.Fatalf("classifyError(%v) = %s, want %s", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsLockError(t *testing.T) {
	if isLockError(nil) {
		t.Fatal("nil is not a lock error")
	}
	if !isLockError(fmt.Errorf("wrap: %w", errors.New("USED BY ANOTHER PROCESS"))) {
		t.Fatal("match must be case-insensitive and see through wrapping")
	}
	if isLockError(errors.New("permission denied")) {
		t.Fatal("permission denied is not a lock")
	}
}
