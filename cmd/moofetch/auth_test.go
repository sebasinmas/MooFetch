package main

import (
	"context"
	"errors"
	"testing"

	"github.com/sebasinmas/MooFetch/internal/auth"
)

type fakeCookies struct {
	val string
	err error
}

func (f fakeCookies) Find(context.Context, string) (string, error) { return f.val, f.err }

func withAuthFlags(t *testing.T, uni, domain string, p fakeCookies) {
	t.Helper()
	oldU, oldD, oldP := flagUni, flagDomain, cookieProvider
	flagUni, flagDomain, cookieProvider = uni, domain, p
	t.Cleanup(func() { flagUni, flagDomain, cookieProvider = oldU, oldD, oldP })
}

func TestResolveHeadlessCookie(t *testing.T) {
	t.Setenv("MOODLE_SESSION", "MoodleSession=env")

	withAuthFlags(t, "ufro", "", fakeCookies{val: "fromBrowser"})
	if got, err := resolveHeadlessCookie(context.Background(), ""); err != nil || got != "MoodleSession=fromBrowser" {
		t.Fatalf("detect: %q %v", got, err)
	}
	if got, _ := resolveHeadlessCookie(context.Background(), "MoodleSession=flag"); got != "MoodleSession=flag" {
		t.Fatalf("explicit cookie must win: %q", got)
	}

	withAuthFlags(t, "ufro", "", fakeCookies{err: errors.New("boom")})
	if got, _ := resolveHeadlessCookie(context.Background(), ""); got != "MoodleSession=env" {
		t.Fatalf("fallback to env: %q", got)
	}

	withAuthFlags(t, "", "", fakeCookies{val: "x"})
	if got, _ := resolveHeadlessCookie(context.Background(), ""); got != "MoodleSession=env" {
		t.Fatalf("no flags -> env: %q", got)
	}

	withAuthFlags(t, "nope", "", fakeCookies{})
	if _, err := resolveHeadlessCookie(context.Background(), ""); err == nil || DetermineExitCode(context.Background(), err) != ExitUsageErr {
		t.Fatalf("unknown uni must be usage error: %v", err)
	}
}

func TestResolveHeadlessCookie_EdgeCases(t *testing.T) {
	t.Setenv("MOODLE_SESSION", "")

	// Provider succeeds but returns nothing: no source available -> empty cookie, no error.
	withAuthFlags(t, "", "campus.example.cl", fakeCookies{})
	if got, err := resolveHeadlessCookie(context.Background(), ""); err != nil || got != "" {
		t.Fatalf("empty detection: %q %v", got, err)
	}

	// Invalid custom domain is a usage error.
	withAuthFlags(t, "", "not a domain", fakeCookies{val: "x"})
	if _, err := resolveHeadlessCookie(context.Background(), ""); !errors.Is(err, auth.ErrInvalidDomain) {
		t.Fatalf("want ErrInvalidDomain, got %v", err)
	}

	// Explicit cookie skips validation entirely.
	if got, err := resolveHeadlessCookie(context.Background(), "tok"); err != nil || got != "MoodleSession=tok" {
		t.Fatalf("explicit: %q %v", got, err)
	}
}

func TestResolveHeadlessCookie_NormalizesPrefix(t *testing.T) {
	withAuthFlags(t, "", "", fakeCookies{})

	for _, in := range []string{"abc123", "MoodleSession=abc123", "Cookie: MoodleSession=abc123", `"abc123"`} {
		if got, err := resolveHeadlessCookie(context.Background(), in); err != nil || got != "MoodleSession=abc123" {
			t.Errorf("explicit %q -> %q %v", in, got, err)
		}
	}

	t.Setenv("MOODLE_SESSION", "abc123")
	if got, _ := resolveHeadlessCookie(context.Background(), ""); got != "MoodleSession=abc123" {
		t.Errorf("env without prefix -> %q", got)
	}

	t.Setenv("MOODLE_SESSION", "")
	if got, _ := resolveHeadlessCookie(context.Background(), ""); got != "" {
		t.Errorf("empty env must stay empty, got %q", got)
	}
}
