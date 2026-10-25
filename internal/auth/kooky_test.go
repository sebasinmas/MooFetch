package auth

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/browserutils/kooky"
)

const campus = "campus.example.cl"

func TestKookyProvider_Find(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		stores  []*fakeStore
		want    string
		wantErr error
	}{
		{"none", nil, "", ErrCookieNotFound},
		{"newest wins across stores", []*fakeStore{
			{browser: "chrome", cookies: []*kooky.Cookie{ck("old", now.Add(-2*time.Hour), time.Time{})}},
			{browser: "edge", cookies: []*kooky.Cookie{
				ck("new", now.Add(-time.Hour), now.Add(time.Hour)),
				ck("older", now.Add(-3*time.Hour), time.Time{}),
			}},
		}, "MoodleSession=new", nil},
		{"skips errors, nil, empty and expired", []*fakeStore{
			{browser: "chrome", err: errors.New("locked db")},
			{browser: "edge", cookies: []*kooky.Cookie{nil, ck("", now, time.Time{}), ck("expired", now, now.Add(-time.Minute))}},
		}, "", ErrCookieNotFound},
		{"valid after a failing store", []*fakeStore{
			{browser: "chrome", err: errors.New("locked db")},
			{browser: "firefox", cookies: []*kooky.Cookie{ck("ok", now, time.Time{})}},
		}, "MoodleSession=ok", nil},
		{"store with cookie and error still counts", []*fakeStore{
			{browser: "firefox", cookies: []*kooky.Cookie{ck("ok", now, time.Time{})}, err: errors.New("partial")},
		}, "MoodleSession=ok", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubStoreList(t, tc.stores...)
			got, err := KookyProvider{}.Find(context.Background(), campus)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %q %v, want %q %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestKookyProvider_Find_ContextCanceled(t *testing.T) {
	stubStoreList(t, &fakeStore{browser: "chrome", cookies: []*kooky.Cookie{ck("v", time.Now(), time.Time{})}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (KookyProvider{}).Find(ctx, campus); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestKookyProvider_Find_KeepsRealCookieName(t *testing.T) {
	c := ck("abc123", time.Now(), time.Time{})
	c.Name = "MoodleSessionufro"
	stubStoreList(t, &fakeStore{browser: "chrome", cookies: []*kooky.Cookie{c}})
	got, err := KookyProvider{}.Find(context.Background(), campus)
	if err != nil || got != "MoodleSessionufro=abc123" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestKookyProvider_Find_MessagesAndHints(t *testing.T) {
	base := ErrCookieNotFound.Error()
	if base != "No se ha encontrado la cookie ¿Ya iniciaste sesión en tu campus virtual?" {
		t.Fatalf("mandated text changed: %q", base)
	}
	missing := &fs.PathError{Op: "open", Path: "/home/u/.local/share/uzbl/session-cookies.txt", Err: fs.ErrNotExist}
	locked := fmt.Errorf("open C:\\Users\\u\\Cookies: %w", errors.New("The process cannot access the file because it is being used by another process."))
	enc := errors.New("decrypting cookie: unsupported v20 App-Bound Encryption (requires elevated COM access)")
	tests := []struct {
		name   string
		stores []*fakeStore
		want   string
	}{
		{"nothing", nil, base},
		{"missing store: no detail", []*fakeStore{{browser: "uzbl", err: missing}}, base},
		{"unreadable: no hint, no raw cause", []*fakeStore{{browser: "chrome", err: errors.New("database is corrupt /secret/path")}}, base},
		{"no cookie: no hint", []*fakeStore{{browser: "chrome"}}, base},
		{"locked", []*fakeStore{{browser: "opera", err: locked}}, base + " " + hintLocked},
		{"encrypted", []*fakeStore{{browser: "chrome", err: enc}}, base + " " + hintEncrypted},
		{"locked wins over encrypted", []*fakeStore{{browser: "chrome", err: enc}, {browser: "opera", err: locked}}, base + " " + hintLocked},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubStoreList(t, tc.stores...)
			_, err := KookyProvider{}.Find(context.Background(), campus)
			if !errors.Is(err, ErrCookieNotFound) {
				t.Fatalf("must wrap ErrCookieNotFound, got %v", err)
			}
			if err.Error() != tc.want {
				t.Fatalf("message = %q, want %q", err.Error(), tc.want)
			}
			for _, leak := range []string{"/", "\\", "used by another", "v20", "corrupt"} {
				if strings.Contains(strings.TrimPrefix(err.Error(), base), leak) {
					t.Errorf("message leaks %q: %q", leak, err.Error())
				}
			}
		})
	}
}

func TestKookyProvider_Find_CallsOnReports(t *testing.T) {
	stubStoreList(t,
		&fakeStore{browser: "chrome", profile: "Default", cookies: []*kooky.Cookie{ck("super-secret", time.Now(), time.Time{})}},
		&fakeStore{browser: "edge"},
	)
	var got []StoreReport
	p := KookyProvider{OnReports: func(r []StoreReport) { got = r }}
	if _, err := p.Find(context.Background(), campus); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 reports, got %+v", got)
	}
	if strings.Contains(fmt.Sprintf("%+v", got), "super-secret") {
		t.Fatal("reports must never contain cookie values")
	}

	// Also called when the search fails.
	got = nil
	stubStoreList(t, &fakeStore{browser: "chrome"})
	if _, err := p.Find(context.Background(), campus); !errors.Is(err, ErrCookieNotFound) {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Outcome != OutcomeNoCookie {
		t.Fatalf("got %+v", got)
	}
}

func TestKookyProvider_Diagnose_ReportsEveryStore(t *testing.T) {
	missing := &fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist}
	stubStores(t,
		storeItem{store: &fakeStore{browser: "chrome", profile: "Default", cookies: []*kooky.Cookie{ck("v", time.Now(), time.Time{})}, strategy: StrategyShared}},
		storeItem{store: &fakeStore{browser: "firefox", profile: "abc.default"}, err: nil},
		storeItem{store: &fakeStore{browser: "edge", err: errors.New("database is locked")}},
		storeItem{store: &fakeStore{browser: "brave", err: errors.New("unsupported v20 App-Bound Encryption")}},
		storeItem{store: &fakeStore{browser: "opera", err: missing}},
		storeItem{store: &fakeStore{browser: "vivaldi", err: errors.New("boom")}},
		storeItem{err: errors.New("cookie store: not implemented")},                            // dropped
		storeItem{err: &fs.PathError{Op: "open", Path: "/x/Local State", Err: fs.ErrNotExist}}, // dropped
		storeItem{err: errors.New("finder exploded")},
	)
	reports := KookyProvider{}.Diagnose(context.Background(), campus)
	want := []struct {
		browser string
		outcome Outcome
	}{
		{"Chrome", OutcomeFound}, {"Firefox", OutcomeNoCookie}, {"Edge", OutcomeLocked},
		{"Brave", OutcomeEncrypted}, {"Opera", OutcomeMissing}, {"Vivaldi", OutcomeUnreadable},
		{unknownBrowser, OutcomeUnreadable},
	}
	if len(reports) != len(want) {
		t.Fatalf("got %d reports: %+v", len(reports), reports)
	}
	for i, w := range want {
		if reports[i].Browser != w.browser || reports[i].Outcome != w.outcome {
			t.Errorf("report %d = %s/%s, want %s/%s", i, reports[i].Browser, reports[i].Outcome, w.browser, w.outcome)
		}
	}
	if reports[0].Profile != "Default" || reports[0].Strategy != StrategyShared || reports[0].Err != nil {
		t.Errorf("unexpected found report: %+v", reports[0])
	}
	if reports[2].Err == nil || reports[2].Strategy != StrategyNone {
		t.Errorf("failed report must keep the error and strategy none: %+v", reports[2])
	}
}

func TestScan_HungStoreDoesNotBlockOthers(t *testing.T) {
	setStoreTimeout(t, 40*time.Millisecond)
	hung := &fakeStore{browser: "chrome", hang: true}
	ok := &fakeStore{browser: "firefox", cookies: []*kooky.Cookie{ck("v", time.Now(), time.Time{})}}
	stubStoreList(t, hung, ok)

	start := time.Now()
	reports, best := scan(context.Background(), campus)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("scan blocked for %v", elapsed)
	}
	if best == nil || best.Value != "v" {
		t.Fatalf("the healthy store must still be read, got %+v", best)
	}
	if len(reports) != 2 || reports[0].Outcome != OutcomeUnreadable || !errors.Is(reports[0].Err, errStoreTimeout) {
		t.Fatalf("hung store must be reported as unreadable/timeout: %+v", reports)
	}
	if reports[1].Outcome != OutcomeFound {
		t.Fatalf("got %+v", reports[1])
	}
	waitClosed(t, hung)
	waitClosed(t, ok)
}

func waitClosed(t *testing.T, s *fakeStore) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := s.closed.Load(); got != 1 {
		t.Fatalf("store %s closed %d times, want exactly 1", s.browser, got)
	}
}

func TestScan_PanickingStoreIsContained(t *testing.T) {
	bad := &fakeStore{browser: "chrome", boom: true}
	stubStoreList(t, bad, &fakeStore{browser: "edge"})
	reports, best := scan(context.Background(), campus)
	if best != nil || len(reports) != 2 {
		t.Fatalf("got %+v %+v", reports, best)
	}
	if reports[0].Outcome != OutcomeUnreadable || strings.Contains(reports[0].Err.Error(), "secret-cookie-value") {
		t.Fatalf("panic must be reported without its value: %+v", reports[0])
	}
	waitClosed(t, bad)
}

func TestScan_DiscoveryThatHangsIsReported(t *testing.T) {
	setStoreTimeout(t, 30*time.Millisecond)
	old := listStores
	listStores = func(ctx context.Context) iter.Seq2[storeReader, error] {
		return func(yield func(storeReader, error) bool) {
			if !yield(&fakeStore{browser: "firefox", cookies: []*kooky.Cookie{ck("v", time.Now(), time.Time{})}}, nil) {
				return
			}
			<-ctx.Done() // a finder that never ends
		}
	}
	t.Cleanup(func() { listStores = old })

	start := time.Now()
	reports, best := scan(context.Background(), campus)
	if time.Since(start) > 2*time.Second {
		t.Fatal("scan blocked by a hung finder")
	}
	if best == nil || len(reports) != 2 || !errors.Is(reports[1].Err, errStoreTimeout) {
		t.Fatalf("got %+v %+v", reports, best)
	}
}

func TestScan_StopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	hung := &fakeStore{browser: "chrome", hang: true}
	stubStoreList(t, hung, &fakeStore{browser: "edge"})
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	reports, _ := scan(ctx, campus)
	if len(reports) != 1 || !errors.Is(reports[0].Err, context.Canceled) {
		t.Fatalf("got %+v", reports)
	}
}

func TestHintFor(t *testing.T) {
	if hintFor(nil) != "" {
		t.Fatal("no reports, no hint")
	}
	if got := hintFor([]StoreReport{{Outcome: OutcomeEncrypted}}); got != hintEncrypted {
		t.Fatalf("got %q", got)
	}
	if got := hintFor([]StoreReport{{Outcome: OutcomeEncrypted}, {Outcome: OutcomeLocked}}); got != hintLocked {
		t.Fatalf("got %q", got)
	}
}

func TestSessionFilter(t *testing.T) {
	f := sessionFilter("campus.example.cl")
	mk := func(name, domain string) *kooky.Cookie {
		c := &kooky.Cookie{}
		c.Name, c.Domain = name, domain
		return c
	}
	for c, want := range map[*kooky.Cookie]bool{
		mk("MoodleSession", "campus.example.cl"):     true,
		mk("MoodleSessionabc", ".example.cl"):        true,
		mk("MoodleSession", "other.cl"):              false,
		mk("Other", "campus.example.cl"):             false,
		mk("MoodleSession", "cl"):                    false,
		mk("MoodleSession", "evilcampus.example.cl"): false,
	} {
		if got := f.Filter(c); got != want {
			t.Errorf("%s@%s: got %v want %v", c.Name, c.Domain, got, want)
		}
	}
}

func TestDisplayBrowser(t *testing.T) {
	if displayBrowser("chrome") != "Chrome" || displayBrowser("") != unknownBrowser {
		t.Fatal("unexpected display names")
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
