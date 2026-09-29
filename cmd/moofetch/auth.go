package main

import (
	"context"
	"fmt"
	"os"

	"moofetch/internal/auth"
	"moofetch/internal/tui"
)

var (
	flagUni    string
	flagDomain string
)

// cookieProvider is the concrete detector wired at the composition root.
// Tests replace it with a fake.
var cookieProvider auth.CookieProvider = auth.KookyProvider{}

// resolveHeadlessCookie returns the cookie for non-interactive runs.
// Precedence: --cookie, browser detection (--uni/--domain), MOODLE_SESSION.
// An empty result means no cookie source was available.
func resolveHeadlessCookie(ctx context.Context, explicit string) (string, error) {
	if explicit != "" {
		return tui.NormalizeCookie(explicit), nil
	}
	domain, err := auth.ResolveDomain(flagUni, flagDomain)
	if err != nil {
		return "", err
	}
	if domain != "" {
		c, derr := auth.Detect(ctx, cookieProvider, domain)
		if derr == nil {
			return c, nil
		}
		fmt.Fprintf(os.Stderr, "No se pudo detectar la cookie de %s: %v\n", domain, derr)
	}
	return tui.NormalizeCookie(os.Getenv("MOODLE_SESSION")), nil
}

// authOptions builds the interactive detection step configuration.
func authOptions() (*tui.AuthOptions, error) {
	domain, err := auth.ResolveDomain(flagUni, flagDomain)
	if err != nil {
		return nil, err
	}
	var unis []tui.University
	for _, u := range auth.Universities() {
		unis = append(unis, tui.University{Name: u.Name, Domain: u.Domain})
	}
	return &tui.AuthOptions{
		Universities:   unis,
		ValidateDomain: auth.ValidateDomain,
		Domain:         domain,
		Notice:         os.Stderr,
		Detect: func(ctx context.Context, d string) (string, error) {
			return auth.Detect(ctx, cookieProvider, d)
		},
	}, nil
}
