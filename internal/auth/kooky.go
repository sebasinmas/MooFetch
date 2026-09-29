package auth

import (
	"context"
	"strings"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all" // registers Chrome/Chromium/Firefox/Edge/... finders
)

// KookyProvider reads the cookie from local browser stores via kooky.
type KookyProvider struct{}

// Find returns the most recently created, non-expired MoodleSession value.
// Per-store read errors are ignored (locked DBs, missing browsers).
func (KookyProvider) Find(ctx context.Context, domain string) (string, error) {
	var best *kooky.Cookie
	now := time.Now()
	for c, err := range kooky.TraverseCookies(ctx, kooky.Name(CookieName), kooky.FilterFunc(func(c *kooky.Cookie) bool { return domainMatches(c.Domain, domain) })) {
		if err != nil || c == nil || c.Value == "" {
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
		return "", ErrCookieNotFound
	}
	return best.Value, nil
}

// domainMatches reports whether a cookie's Domain attribute applies to host
// (exact match, ".host", or a parent domain cookie such as ".ufro.cl").
func domainMatches(cookieDomain, host string) bool {
	cd := strings.ToLower(strings.TrimPrefix(cookieDomain, "."))
	return strings.Contains(cd, ".") && (host == cd || strings.HasSuffix(host, "."+cd))
}
