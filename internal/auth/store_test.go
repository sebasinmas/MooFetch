package auth

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/browserutils/kooky"
)

// fakeKooky is a kooky.CookieStore that never touches a real browser.
type fakeKooky struct {
	browser, path string
	cookies       []*kooky.Cookie
	errs          []error
	closed        atomic.Int32
}

func (f *fakeKooky) SetCookies(*url.URL, []*http.Cookie) {}
func (f *fakeKooky) Cookies(*url.URL) []*http.Cookie     { return nil }
func (f *fakeKooky) Browser() string                     { return f.browser }
func (f *fakeKooky) Profile() string                     { return "Default" }
func (f *fakeKooky) IsDefaultProfile() bool              { return true }
func (f *fakeKooky) FilePath() string                    { return f.path }
func (f *fakeKooky) Close() error                        { f.closed.Add(1); return nil }
func (f *fakeKooky) SubJar(context.Context, ...kooky.Filter) (http.CookieJar, error) {
	return nil, errors.New("not used")
}

func (f *fakeKooky) TraverseCookies(...kooky.Filter) kooky.CookieSeq {
	return func(yield func(*kooky.Cookie, error) bool) {
		for _, e := range f.errs {
			if !yield(nil, e) {
				return
			}
		}
		for _, c := range f.cookies {
			if !yield(c, nil) {
				return
			}
		}
	}
}

func stubKooky(t *testing.T, items ...func(yield func(kooky.CookieStore, error) bool) bool) {
	t.Helper()
	old := kookyTraverse
	kookyTraverse = func(context.Context) kooky.CookieStoreSeq {
		return func(yield func(kooky.CookieStore, error) bool) {
			for _, it := range items {
				if !it(yield) {
					return
				}
			}
		}
	}
	t.Cleanup(func() { kookyTraverse = old })
}

func store(s kooky.CookieStore) func(func(kooky.CookieStore, error) bool) bool {
	return func(y func(kooky.CookieStore, error) bool) bool { return y(s, nil) }
}

func failure(err error) func(func(kooky.CookieStore, error) bool) bool {
	return func(y func(kooky.CookieStore, error) bool) bool { return y(nil, err) }
}

func TestKookyStore_Read(t *testing.T) {
	good := ck("v", time.Now(), time.Time{})
	ctx := context.Background()

	cookies, strategy, err := kookyStore{&fakeKooky{browser: "chrome", cookies: []*kooky.Cookie{good, nil}}}.Read(ctx, allCookies())
	if err != nil || strategy != StrategyShared || len(cookies) != 1 {
		t.Fatalf("%v %s %v", cookies, strategy, err)
	}

	boom := errors.New("decrypting: unsupported v20 App-Bound Encryption")
	cookies, strategy, err = kookyStore{&fakeKooky{errs: []error{boom, errors.New("second")}}}.Read(ctx, allCookies())
	if !errors.Is(err, boom) || strategy != StrategyNone || cookies != nil {
		t.Fatalf("only errors: %v %s %v", cookies, strategy, err)
	}

	cookies, strategy, err = kookyStore{&fakeKooky{errs: []error{boom}, cookies: []*kooky.Cookie{good}}}.Read(ctx, allCookies())
	if !errors.Is(err, boom) || strategy != StrategyShared || len(cookies) != 1 {
		t.Fatalf("cookie and error: %v %s %v", cookies, strategy, err)
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if cookies, _, _ := (kookyStore{&fakeKooky{cookies: []*kooky.Cookie{good}}}).Read(cctx, allCookies()); len(cookies) != 0 {
		t.Fatalf("a cancelled context must stop the read, got %v", cookies)
	}
}

func collect(seq iter.Seq2[storeReader, error]) (stores []storeReader, errs []error) {
	for s, err := range seq {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		stores = append(stores, s)
	}
	return stores, errs
}

func TestDefaultStores_FirefoxDedupeAndPassThrough(t *testing.T) {
	dir := fakeFirefoxProfile(t, "recovery.jsonlz4", mozLz4(t, []byte(sessionJSON)))
	covered := &fakeKooky{browser: "firefox", path: filepath.Join(dir, "cookies.sqlite")}
	foreign := &fakeKooky{browser: "Firefox", path: filepath.Join(t.TempDir(), "other.default", "cookies.sqlite")}
	chrome := &fakeKooky{browser: "chrome", path: "/x/Cookies"}
	finderErr := errors.New("finder failed")
	stubKooky(t, store(covered), failure(finderErr), store(nil), store(chrome), store(foreign))

	stores, errs := collect(defaultStores(context.Background()))
	if len(errs) != 1 || !errors.Is(errs[0], finderErr) {
		t.Fatalf("errors must pass through: %v", errs)
	}
	var names []string
	for _, s := range stores {
		names = append(names, s.Browser()+"/"+s.Profile())
	}
	want := "firefox/abc.default-release,chrome/Default,Firefox/Default"
	if strings.Join(names, ",") != want {
		t.Fatalf("stores = %v, want %s", names, want)
	}
	if covered.closed.Load() != 1 {
		t.Fatal("a kooky Firefox store already covered by ours must be skipped AND closed")
	}
	if chrome.closed.Load() != 0 || foreign.closed.Load() != 0 {
		t.Fatal("delivered stores belong to the consumer and must not be closed")
	}
}

func TestDefaultStores_StopsWhenConsumerStops(t *testing.T) {
	fakeFirefoxProfile(t, "recovery.jsonlz4", []byte("x"))
	chrome := &fakeKooky{browser: "chrome"}
	stubKooky(t, store(chrome))

	n := 0
	for range defaultStores(context.Background()) { // stops while still in our Firefox profiles
		n++
		break
	}
	if n != 1 {
		t.Fatalf("n=%d", n)
	}

	n = 0
	for s := range defaultStores(context.Background()) { // stops at the first kooky store
		n++
		if s.Browser() == "chrome" {
			break
		}
	}
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
	stubKooky(t, failure(errors.New("x")), store(chrome))
	for _, err := range defaultStores(context.Background()) { // stops at a finder error
		if err != nil {
			break
		}
	}
}

func TestDiscoverStores_ClosesUndeliveredStores(t *testing.T) {
	a, b := &fakeStore{browser: "chrome"}, &fakeStore{browser: "edge"}
	stubStoreList(t, a, b)
	items, stop := discoverStores(context.Background())
	stop() // nobody consumes: the producer must not leak the store it already opened
	_ = items
	deadline := time.Now().Add(2 * time.Second)
	for a.closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.closed.Load() != 1 || b.closed.Load() != 0 {
		t.Fatalf("closed a=%d b=%d (only the store in hand is closed)", a.closed.Load(), b.closed.Load())
	}
}

func TestScan_ProducerPanicIsContained(t *testing.T) {
	old := listStores
	t.Cleanup(func() { listStores = old })

	listStores = func(context.Context) iter.Seq2[storeReader, error] {
		return func(yield func(storeReader, error) bool) {
			if !yield(&fakeStore{browser: "firefox", cookies: []*kooky.Cookie{ck("v", time.Now(), time.Time{})}}, nil) {
				return
			}
			panic("secret-cookie-value")
		}
	}
	reports, best := scan(context.Background(), campus)
	if best == nil || len(reports) != 2 || reports[1].Outcome != OutcomeUnreadable || !errors.Is(reports[1].Err, errInternalRead) {
		t.Fatalf("got %+v %+v", reports, best)
	}
	if strings.Contains(strings.ToLower(reports[1].Err.Error()), "secret") {
		t.Fatal("the panic value must not be echoed")
	}

	listStores = func(context.Context) iter.Seq2[storeReader, error] { panic("finder exploded") }
	reports, best = scan(context.Background(), campus)
	if best != nil || len(reports) != 1 || !errors.Is(reports[0].Err, errInternalRead) {
		t.Fatalf("a panic while starting discovery must be contained: %+v", reports)
	}
}

func TestScan_PanickingCloseIsContained(t *testing.T) {
	bad := &fakeStore{browser: "chrome", boomClose: true, cookies: []*kooky.Cookie{ck("v", time.Now(), time.Time{})}}
	stubStoreList(t, bad, &fakeStore{browser: "edge"})
	reports, best := scan(context.Background(), campus)
	if best == nil || len(reports) != 2 || reports[0].Outcome != OutcomeFound {
		t.Fatalf("a panic in Close must not lose the result: %+v", reports)
	}
	waitClosed(t, bad)

	// And for a store the producer never delivered.
	undelivered := &fakeStore{browser: "x", boomClose: true}
	stubStoreList(t, undelivered)
	_, stop := discoverStores(context.Background())
	stop()
	deadline := time.Now().Add(2 * time.Second)
	for undelivered.closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

func TestScan_BudgetBoundsSeveralHungStores(t *testing.T) {
	setStoreTimeout(t, 10*time.Second) // per-store timeout alone would take far longer
	old := scanBudget
	scanBudget = 100 * time.Millisecond
	t.Cleanup(func() { scanBudget = old })
	hung := []*fakeStore{{browser: "chrome", hang: true}, {browser: "edge", hang: true}, {browser: "brave", hang: true}, {browser: "opera", hang: true}}
	stubStoreList(t, hung...)

	start := time.Now()
	reports, best := scan(context.Background(), campus)
	if elapsed := time.Since(start); elapsed > scanBudget+time.Second {
		t.Fatalf("scan took %v with a budget of %v", elapsed, scanBudget)
	}
	if best != nil || len(reports) != 2 {
		t.Fatalf("got %+v", reports)
	}
	for _, r := range reports {
		if r.Outcome != OutcomeUnreadable || !errors.Is(r.Err, errScanBudget) {
			t.Errorf("report must say the total time ran out: %+v", r)
		}
	}
	waitClosed(t, hung[0])
}

func TestScan_ParentCancelIsNotABudgetReport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stubStoreList(t, &fakeStore{browser: "chrome"})
	reports, _ := scan(ctx, campus)
	for _, r := range reports {
		if errors.Is(r.Err, errScanBudget) {
			t.Fatalf("a cancelled caller is not a spent budget: %+v", r)
		}
	}
}

func TestScan_StoreThatIgnoresContextDoesNotBlockOrDeadlock(t *testing.T) {
	setStoreTimeout(t, 30*time.Millisecond)
	stuck := &fakeStore{browser: "chrome", release: make(chan struct{})}
	ok := &fakeStore{browser: "firefox", cookies: []*kooky.Cookie{ck("v", time.Now(), time.Time{})}}
	stubStoreList(t, stuck, ok)

	reports, best := scan(context.Background(), campus)
	if best == nil || len(reports) != 2 || !errors.Is(reports[0].Err, errStoreTimeout) {
		t.Fatalf("got %+v %+v", reports, best)
	}
	close(stuck.release) // the abandoned reader now finishes: its result goes to a buffered channel
	waitClosed(t, stuck)
}

func TestBuildReport_FoundKeepsSecondaryError(t *testing.T) {
	s := &fakeStore{browser: "firefox", profile: "p"}
	secondary := errors.New("no se pudo borrar la carpeta temporal")
	rep, c := buildReport(s, readOutcome{cookies: []*kooky.Cookie{ck("v", time.Now(), time.Time{})}, strategy: StrategyCopy, err: secondary}, time.Now())
	if c == nil || rep.Outcome != OutcomeFound || !errors.Is(rep.Err, secondary) || rep.Strategy != StrategyCopy {
		t.Fatalf("%+v", rep)
	}
}

func TestFirefoxProfile_CleanupFailureReachesTheReport(t *testing.T) {
	noSleep(t)
	trackTempDirs(t)
	dir := fakeFirefoxProfile(t, "recovery.jsonlz4", mozLz4(t, []byte(sessionJSON)))
	failOpener(t, lockRetries+1)
	oldRemove := removeAll
	removeAll = func(d string) error { _ = oldRemove(d); return errors.New("Access is denied.") }
	t.Cleanup(func() { removeAll = oldRemove })

	stubStores(t, storeItem{store: firefoxProfile{dir: dir}})
	reports := KookyProvider{}.Diagnose(context.Background(), "campusvirtual.ufro.cl")
	if len(reports) != 1 || reports[0].Outcome != OutcomeFound || reports[0].Strategy != StrategyCopy || !errors.Is(reports[0].Err, errTempCleanup) {
		t.Fatalf("%+v", reports)
	}
}
