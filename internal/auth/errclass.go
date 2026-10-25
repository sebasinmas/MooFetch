package auth

import (
	"errors"
	"io/fs"
	"strings"
)

// errStoreTimeout means a browser store did not answer in time and was
// abandoned so the other browsers could still be read.
var errStoreTimeout = errors.New("tiempo agotado leyendo el almacén de cookies")

// lockMarkers are fragments of the OS/SQLite messages that mean "the file is
// in use". They are matched defensively (lower case, English and Spanish)
// because some libraries flatten the error and lose the errno.
var lockMarkers = []string{
	"used by another process",
	"utilizado por otro proceso",
	"usado por otro proceso",
	"en uso por otro proceso",
	"sharing violation",
	"lock violation",
	"violación de uso compartido",
	"violación de bloqueo",
	"database is locked",
}

// isLockError reports whether err means another process holds the file.
func isLockError(err error) bool {
	if err == nil {
		return false
	}
	if isOSLockError(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, m := range lockMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// isEncryptedError reports Chrome/Edge "v20" App-Bound Encryption errors. It
// matches kooky's phrases, not a bare "v20": that text can appear in file
// paths (Firefox profile folders have random names).
func isEncryptedError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "app-bound") || strings.Contains(msg, "unsupported v20")
}

// classifyError maps a read error to an Outcome. A nil error is not
// classified here: the caller decides between found and no-cookie.
func classifyError(err error) Outcome {
	switch {
	case err == nil:
		return OutcomeNoCookie
	case errors.Is(err, fs.ErrNotExist):
		return OutcomeMissing
	case isLockError(err): // same priority as the hints: a closed browser can be fixed
		return OutcomeLocked
	case isEncryptedError(err):
		return OutcomeEncrypted
	default:
		return OutcomeUnreadable
	}
}
