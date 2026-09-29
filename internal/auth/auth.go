// Package auth detects the Moodle session cookie from local browsers.
//
// It must not import kernel or tui; the composition root (cmd/moofetch) wires
// it into the form and the headless mode. Cookie values are never logged nor
// persisted by this package.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

// CookieName is the Moodle session cookie name.
const CookieName = "MoodleSession"

var (
	// ErrCookieNotFound means no session cookie exists for the domain.
	ErrCookieNotFound = errors.New("no se encontró la cookie de sesión en los navegadores locales")
	// ErrUnsupported means browser detection is unavailable in this build.
	ErrUnsupported = errors.New("la detección automática no está disponible en esta compilación")
	// ErrInvalidDomain means the supplied domain is not a valid host name.
	ErrInvalidDomain = errors.New("dominio inválido")
	// ErrUnknownUniversity means the university key is not in the catalog.
	ErrUnknownUniversity = errors.New("universidad desconocida")
)

// CookieProvider finds the raw session cookie value for a domain.
type CookieProvider interface {
	Find(ctx context.Context, domain string) (string, error)
}

// University is a catalog entry.
type University struct {
	Key    string
	Name   string
	Domain string
}

var catalog = []University{
	{Key: "ufro", Name: "Universidad de La Frontera (UFRO)", Domain: "campusvirtual.ufro.cl"},
}

// Universities returns the catalog sorted by key.
func Universities() []University {
	out := append([]University(nil), catalog...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// DomainForUniversity resolves a catalog key to its Moodle domain.
func DomainForUniversity(key string) (string, error) {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, u := range catalog {
		if u.Key == k {
			return u.Domain, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownUniversity, key)
}

// ValidateDomain normalizes and validates a custom host name. It accepts an
// optional scheme/path (e.g. https://campus.example/x) and returns the bare
// lowercase host.
func ValidateDomain(raw string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	if i := strings.IndexAny(d, "/?#"); i >= 0 {
		d = d[:i]
	}
	d = strings.TrimSuffix(d, ".")
	if d == "" || len(d) > 253 || net.ParseIP(d) != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidDomain, raw)
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("%w: %q", ErrInvalidDomain, raw)
	}
	for _, l := range labels {
		if !validLabel(l) {
			return "", fmt.Errorf("%w: %q", ErrInvalidDomain, raw)
		}
	}
	return d, nil
}

func validLabel(l string) bool {
	if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
		return false
	}
	for _, r := range l {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// ResolveDomain picks the domain from a university key or a custom domain.
// The custom domain wins when both are given; both empty returns "", nil.
func ResolveDomain(uni, domain string) (string, error) {
	if strings.TrimSpace(domain) != "" {
		return ValidateDomain(domain)
	}
	if strings.TrimSpace(uni) != "" {
		return DomainForUniversity(uni)
	}
	return "", nil
}

// Detect asks the provider for the session cookie and returns it in
// "MoodleSession=<value>" form.
func Detect(ctx context.Context, p CookieProvider, domain string) (string, error) {
	if p == nil {
		return "", ErrUnsupported
	}
	d, err := ValidateDomain(domain)
	if err != nil {
		return "", err
	}
	v, err := p.Find(ctx, d)
	if err != nil {
		return "", err
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", ErrCookieNotFound
	}
	return CookieName + "=" + v, nil
}
