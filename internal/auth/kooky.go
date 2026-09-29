package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all" // registers Chrome/Chromium/Firefox/Edge/... finders
)

// KookyProvider reads the cookie from local browser stores via kooky.
type KookyProvider struct{}

// traverseCookies is the seam over kooky's browser stores; tests replace it so
// no real browser profile is ever read.
//
// Moodle lets each site add a suffix to the cookie name (for example
// "MoodleSessionufro"), so the filter matches by prefix, not by exact name.
var traverseCookies = func(ctx context.Context, domain string) kooky.CookieSeq {
	filter := kooky.FilterFunc(func(c *kooky.Cookie) bool {
		return isSessionCookieName(c.Name) && domainMatches(c.Domain, domain)
	})
	return func(yield func(*kooky.Cookie, error) bool) {
		for c, err := range kooky.TraverseCookies(ctx, filter) {
			if !yield(c, err) {
				return
			}
		}
		// Firefox session cookies and newer profile folders (see firefox.go).
		for c, err := range firefoxExtraCookies(ctx, filter) {
			if !yield(c, err) {
				return
			}
		}
	}
}

// ignorableStoreError reports read errors that only mean "this browser type does
// not exist on this system" (for example Internet Explorer on Linux). They must
// not hide the real reason in the message shown to the user.
func ignorableStoreError(err error) bool {
	return strings.Contains(err.Error(), "not implemented")
}

// Find returns the most recently created, non-expired session cookie as
// "Name=value", using the real cookie name found in the browser.
// Per-store read errors do not stop the search, but the first one is added to
// the "not found" error so the user can see why (locked database, keyring...).
func (KookyProvider) Find(ctx context.Context, domain string) (string, error) {
	var best *kooky.Cookie
	var firstErr error
	now := time.Now()
	for c, err := range traverseCookies(ctx, domain) {
		if err != nil {
			if firstErr == nil && !ignorableStoreError(err) {
				firstErr = err
			}
			continue
		}
		if c == nil || c.Value == "" {
			continue
		}
		if !c.Expires.IsZero() && c.Expires.Before(now) {
			continue
		}
		if best == nil || c.Creation.After(best.Creation) {
			best = c
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if best == nil {
		if firstErr != nil {
			return "", fmt.Errorf("%w (error al leer el navegador: %v)", ErrCookieNotFound, firstErr)
		}
		return "", ErrCookieNotFound
	}
	return best.Name + "=" + best.Value, nil
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
