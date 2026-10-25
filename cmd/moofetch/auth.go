package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sebasinmas/MooFetch/internal/auth"
	"github.com/sebasinmas/MooFetch/internal/plugins/moodle"
	"github.com/sebasinmas/MooFetch/internal/tui"
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
		fmt.Fprintf(os.Stderr, "%v\n", derr)
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
	opts := &tui.AuthOptions{
		Universities:   unis,
		ValidateDomain: auth.ValidateDomain,
		Domain:         domain,
		Notice:         os.Stderr,
		Scan:           scanForTUI,
		Detect: func(ctx context.Context, d string) (string, error) {
			return auth.Detect(ctx, cookieProvider, d)
		},
	}
	if flagDemo {
		opts = withDemo(opts)
	}
	return opts, nil
}

// scanForTUI lists a course page's resources for the interactive selection.
func scanForTUI(ctx context.Context, pageURL, cookie string) ([]tui.Resource, error) {
	links, err := scanResources(ctx, moodle.New(), pageURL, cookie,
		moodle.ScanOptions{Delay: flagScanDelay, MaxPages: flagScanMaxPages})
	if err != nil {
		return nil, err
	}
	found := make([]tui.Resource, len(links))
	for i, l := range links {
		found[i] = tui.Resource{Title: l.Title, URL: l.URL}
	}
	return found, nil
}
