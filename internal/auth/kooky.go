package auth

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// KookyProvider reads the cookie from local browser stores (via kooky for the
// Chromium family and Safari, and our own reader for Firefox).
//
// Every browser profile is read on its own, one after another, each with a
// timeout, so one locked, encrypted or hung browser never hides the others.
// What happened to each one is available as []StoreReport (see Diagnose and
// OnReports); reports never contain cookie values.
type KookyProvider struct {
	// OnReports, if set, is called by Find with the per-store reports of that
	// search (also when it fails). The composition root uses it for logging.
	OnReports func([]StoreReport)
}

var _ CookieProvider = KookyProvider{}
var _ Diagnoser = KookyProvider{}

// Messages appended to ErrCookieNotFound when the reports explain the failure.
// They never include raw OS errors, paths or cookie data.
const (
	hintLocked    = "Un navegador tiene sus archivos en uso: ciérralo o pégala a mano."
	hintEncrypted = "Chrome y Edge protegen sus cookies en Windows: prueba con Firefox o pégala a mano."
)

// ignorableStoreError reports read errors that only mean "this browser type does
// not exist on this system" (for example Internet Explorer on Linux, or a
// browser that is not installed so its cookie file is missing).
func ignorableStoreError(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), "not implemented")
}

// Find returns the most recently created, non-expired session cookie as
// "Name=value", using the real cookie name found in the browser.
// If there is none, the error satisfies errors.Is(err, ErrCookieNotFound) and
// may carry one short hint chosen from the reports.
func (p KookyProvider) Find(ctx context.Context, domain string) (string, error) {
	reports, best := scan(ctx, domain)
	if p.OnReports != nil {
		p.OnReports(reports)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if best == nil {
		return "", notFoundError(reports)
	}
	return best.Name + "=" + best.Value, nil
}

// Diagnose reports, for every browser profile found, what happened when
// looking for the campus session cookie. It never returns cookie values.
func (KookyProvider) Diagnose(ctx context.Context, domain string) []StoreReport {
	reports, _ := scan(ctx, domain)
	return reports
}

// notFoundError returns ErrCookieNotFound, followed by at most one hint when
// the reports show a cause the user can act on.
func notFoundError(reports []StoreReport) error {
	if hint := hintFor(reports); hint != "" {
		return fmt.Errorf("%w %s", ErrCookieNotFound, hint)
	}
	return ErrCookieNotFound
}

// hintFor picks the most actionable hint: a locked browser can be closed, an
// encrypted one cannot be fixed.
func hintFor(reports []StoreReport) string {
	var locked, encrypted bool
	for _, r := range reports {
		switch r.Outcome {
		case OutcomeLocked:
			locked = true
		case OutcomeEncrypted:
			encrypted = true
		}
	}
	switch {
	case locked:
		return hintLocked
	case encrypted:
		return hintEncrypted
	}
	return ""
}

// isSessionCookieName reports whether name is MoodleSession or MoodleSession<suffix>.
func isSessionCookieName(name string) bool {
	return strings.HasPrefix(name, CookieName)
}

// domainMatches reports whether a cookie's Domain attribute applies to host
// (exact match, ".host", or a parent domain cookie such as ".ufro.cl").
func domainMatches(cookieDomain, host string) bool {
	cd := strings.ToLower(strings.TrimPrefix(cookieDomain, "."))
	return strings.Contains(cd, ".") && (host == cd || strings.HasSuffix(host, "."+cd))
}
