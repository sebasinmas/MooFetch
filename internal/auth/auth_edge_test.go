package auth

import (
	"context"
	"errors"
	"testing"
)

type ctxProvider struct{}

func (ctxProvider) Find(ctx context.Context, _ string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "v", nil
}

func TestDetect_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Detect(ctx, ctxProvider{}, "campus.example.cl"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestDetect_ProviderErrorsPropagate(t *testing.T) {
	boom := errors.New("store locked")
	if _, err := Detect(context.Background(), &fakeProvider{err: boom}, "campus.example.cl"); !errors.Is(err, boom) {
		t.Fatalf("want provider error, got %v", err)
	}
}

func TestDetect_WhitespaceOnlyValueIsNotFound(t *testing.T) {
	for _, v := range []string{"", " ", "\t\n"} {
		if _, err := Detect(context.Background(), &fakeProvider{val: v}, "campus.example.cl"); !errors.Is(err, ErrCookieNotFound) {
			t.Errorf("%q: want ErrCookieNotFound, got %v", v, err)
		}
	}
}

func TestDetect_NonexistentDomainReachesProviderAsNotFound(t *testing.T) {
	p := &fakeProvider{err: ErrCookieNotFound}
	_, err := Detect(context.Background(), p, "no-existe.example.invalid")
	if !errors.Is(err, ErrCookieNotFound) || p.got != "no-existe.example.invalid" {
		t.Fatalf("err=%v got=%q", err, p.got)
	}
}

func TestDetect_InvalidDomainDoesNotCallProvider(t *testing.T) {
	p := &fakeProvider{val: "x"}
	if _, err := Detect(context.Background(), p, "127.0.0.1"); !errors.Is(err, ErrInvalidDomain) || p.got != "" {
		t.Fatalf("err=%v provider called with %q", err, p.got)
	}
}

func TestUniversities_SortedAndCopied(t *testing.T) {
	u := Universities()
	if len(u) == 0 {
		t.Fatal("empty catalog")
	}
	u[0].Domain = "mutated.example"
	if d, _ := DomainForUniversity(u[0].Key); d == "mutated.example" {
		t.Fatal("catalog exposed for mutation")
	}
}

func TestDomainMatches_Extra(t *testing.T) {
	for _, c := range []struct {
		cd, host string
		want     bool
	}{
		{"CampusVirtual.UFRO.cl", "campusvirtual.ufro.cl", true},
		{"other.ufro.cl", "campusvirtual.ufro.cl", false},
		{"ufro.cl.evil.com", "campusvirtual.ufro.cl", false},
	} {
		if got := domainMatches(c.cd, c.host); got != c.want {
			t.Errorf("%q %q = %v", c.cd, c.host, got)
		}
	}
}
