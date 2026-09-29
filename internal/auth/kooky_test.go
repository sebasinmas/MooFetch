package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/browserutils/kooky"
)

type fakeEntry struct {
	c   *kooky.Cookie
	err error
}

func stubTraverse(t *testing.T, entries ...fakeEntry) {
	t.Helper()
	old := traverseCookies
	traverseCookies = func(context.Context, string) kooky.CookieSeq {
		return func(yield func(*kooky.Cookie, error) bool) {
			for _, e := range entries {
				if !yield(e.c, e.err) {
					return
				}
			}
		}
	}
	t.Cleanup(func() { traverseCookies = old })
}

func ck(val string, created time.Time, expires time.Time) *kooky.Cookie {
	c := &kooky.Cookie{}
	c.Name, c.Value, c.Creation, c.Expires = CookieName, val, created, expires
	return c
}

func TestKookyProvider_Find(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		entries []fakeEntry
		want    string
		wantErr error
	}{
		{"none", nil, "", ErrCookieNotFound},
		{"newest wins", []fakeEntry{
			{c: ck("old", now.Add(-2*time.Hour), time.Time{})},
			{c: ck("new", now.Add(-time.Hour), now.Add(time.Hour))},
			{c: ck("older", now.Add(-3*time.Hour), time.Time{})},
		}, "MoodleSession=new", nil},
		{"skips errors, nil, empty and expired", []fakeEntry{
			{err: errors.New("locked db")},
			{c: nil},
			{c: ck("", now, time.Time{})},
			{c: ck("expired", now, now.Add(-time.Minute))},
		}, "", ErrCookieNotFound},
		{"valid after bad entries", []fakeEntry{
			{err: errors.New("locked db")},
			{c: ck("ok", now, time.Time{})},
		}, "MoodleSession=ok", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubTraverse(t, tc.entries...)
			got, err := KookyProvider{}.Find(context.Background(), "campus.example.cl")
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %q %v, want %q %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestKookyProvider_Find_ContextCanceled(t *testing.T) {
	stubTraverse(t, fakeEntry{c: ck("v", time.Now(), time.Time{})})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (KookyProvider{}).Find(ctx, "campus.example.cl"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestKookyProvider_Find_KeepsRealCookieName(t *testing.T) {
	c := ck("abc123", time.Now(), time.Time{})
	c.Name = "MoodleSessionufro"
	stubTraverse(t, fakeEntry{c: c})
	got, err := KookyProvider{}.Find(context.Background(), "campus.example.cl")
	if err != nil || got != "MoodleSessionufro=abc123" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestKookyProvider_Find_ReportsReadErrorWhenNothingFound(t *testing.T) {
	stubTraverse(t, fakeEntry{err: errors.New("database is locked")})
	_, err := KookyProvider{}.Find(context.Background(), "campus.example.cl")
	if !errors.Is(err, ErrCookieNotFound) || !strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("error should wrap ErrCookieNotFound and mention the cause, got %v", err)
	}
}

func TestIsSessionCookieName(t *testing.T) {
	for name, want := range map[string]bool{
		"MoodleSession": true, "MoodleSessionufro": true,
		"moodlesession": false, "OtherCookie": false, "": false,
	} {
		if got := isSessionCookieName(name); got != want {
			t.Errorf("%q: got %v want %v", name, got, want)
		}
	}
}

func TestUniversities_MultiEntrySort(t *testing.T) {
	old := catalog
	t.Cleanup(func() { catalog = old })
	catalog = []University{{Key: "b", Domain: "b.cl"}, {Key: "a", Domain: "a.cl"}}
	got := Universities()
	if len(got) != 2 || got[0].Key != "a" || got[1].Key != "b" {
		t.Fatalf("not sorted: %+v", got)
	}
	got[0].Key = "mutated"
	if catalog[1].Key != "a" {
		t.Fatal("Universities must return a copy")
	}
}
