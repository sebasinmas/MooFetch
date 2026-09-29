package auth

import (
	"context"
	"errors"
	"testing"
)

type fakeProvider struct {
	val string
	err error
	got string
}

func (f *fakeProvider) Find(_ context.Context, d string) (string, error) {
	f.got = d
	return f.val, f.err
}

func TestDetect(t *testing.T) {
	p := &fakeProvider{val: " abc123 "}
	got, err := Detect(context.Background(), p, "https://CampusVirtual.ufro.cl/x")
	if err != nil || got != "MoodleSession=abc123" || p.got != "campusvirtual.ufro.cl" {
		t.Fatalf("got %q %v domain %q", got, err, p.got)
	}
	if _, err := Detect(context.Background(), &fakeProvider{err: ErrCookieNotFound}, "a.cl"); !errors.Is(err, ErrCookieNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
	if _, err := Detect(context.Background(), &fakeProvider{}, "a.cl"); !errors.Is(err, ErrCookieNotFound) {
		t.Fatalf("empty value: %v", err)
	}
	if _, err := Detect(context.Background(), nil, "a.cl"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("nil provider: %v", err)
	}
	if _, err := Detect(context.Background(), p, "bad domain"); !errors.Is(err, ErrInvalidDomain) {
		t.Fatalf("bad domain: %v", err)
	}
}

func TestValidateDomain(t *testing.T) {
	good := map[string]string{"campus.example.cl": "campus.example.cl", " HTTPS://Campus.Example.cl/a?b ": "campus.example.cl", "a-b.cl.": "a-b.cl"}
	for in, want := range good {
		if got, err := ValidateDomain(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "localhost", "127.0.0.1", "-a.cl", "a..cl", "a_b.cl", "a b.cl", "x.cl:8080"} {
		if _, err := ValidateDomain(in); !errors.Is(err, ErrInvalidDomain) {
			t.Errorf("%q should be invalid, got %v", in, err)
		}
	}
}

func TestResolveDomain(t *testing.T) {
	if d, err := ResolveDomain("UFRO", ""); err != nil || d != "campusvirtual.ufro.cl" {
		t.Fatalf("%q %v", d, err)
	}
	if d, _ := ResolveDomain("ufro", "otro.example.cl"); d != "otro.example.cl" {
		t.Fatalf("custom should win: %q", d)
	}
	if d, err := ResolveDomain("", ""); d != "" || err != nil {
		t.Fatalf("%q %v", d, err)
	}
	if _, err := ResolveDomain("nope", ""); !errors.Is(err, ErrUnknownUniversity) {
		t.Fatalf("%v", err)
	}
}

func TestDomainMatches(t *testing.T) {
	cases := []struct {
		cd, host string
		want     bool
	}{
		{"campusvirtual.ufro.cl", "campusvirtual.ufro.cl", true},
		{".campusvirtual.ufro.cl", "campusvirtual.ufro.cl", true},
		{".ufro.cl", "campusvirtual.ufro.cl", true},
		{".cl", "campusvirtual.ufro.cl", false},
		{"evil.cl", "campusvirtual.ufro.cl", false},
		{"", "a.cl", false},
	}
	for _, c := range cases {
		if got := domainMatches(c.cd, c.host); got != c.want {
			t.Errorf("%q %q = %v", c.cd, c.host, got)
		}
	}
}
